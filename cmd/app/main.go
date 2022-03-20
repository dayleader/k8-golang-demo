package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	apiv1 "k8-golang-demo/gen/pb-go/com.example/usersvcapi/v1"

	"github.com/google/uuid"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	run "github.com/oklog/oklog/pkg/group"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	// justifying it
	_ "github.com/go-sql-driver/mysql"
)

func main() {

	// Flags.
	//
	fs := flag.NewFlagSet("", flag.ExitOnError)
	grpcAddr := fs.String("grpc-addr", ":6565", "grpc address")
	httpAddr := fs.String("http-addr", ":8080", "http address")
	if err := fs.Parse(os.Args[1:]); err != nil {
		log.Fatal(err)
	}

	// Setup database.
	//
	db, err := NewDatabase(
		os.Getenv("DATABASE_DRIVER"),
		os.Getenv("DATABASE_NAME"),
		os.Getenv("DATABASE_USERNAME"),
		os.Getenv("DATABASE_PASSWORD"),
		os.Getenv("DATABASE_HOST"),
		os.Getenv("DATABASE_PORT"),
	)
	if err != nil {
		log.Fatal(err)
	}
	conn := db.GetConnection()
	defer func() {
		_ = db.CloseConnection()
	}()

	// Setup gRPC servers.
	//
	baseGrpcServer := grpc.NewServer()
	userGrpcServer := NewUserGRPCServer(conn, "users")
	apiv1.RegisterUserServiceServer(baseGrpcServer, userGrpcServer)

	// Setup gRPC gateway.
	//
	ctx := context.Background()
	rmux := runtime.NewServeMux()
	mux := http.NewServeMux()
	mux.Handle("/", rmux)
	{
		err := apiv1.RegisterUserServiceHandlerServer(ctx, rmux, userGrpcServer)
		if err != nil {
			log.Fatal(err)
		}
	}

	// Serve.
	//
	var g run.Group
	{
		grpcListener, err := net.Listen("tcp", *grpcAddr)
		if err != nil {
			log.Fatal(err)
		}
		g.Add(func() error {
			log.Printf("Serving grpc address %s", *grpcAddr)
			return baseGrpcServer.Serve(grpcListener)
		}, func(error) {
			grpcListener.Close()
		})
	}
	{
		httpListener, err := net.Listen("tcp", *httpAddr)
		if err != nil {
			log.Fatal(err)
		}
		g.Add(func() error {
			log.Printf("Serving http address %s", *httpAddr)
			return http.Serve(httpListener, mux)
		}, func(err error) {
			httpListener.Close()
		})
	}
	{
		cancelInterrupt := make(chan struct{})
		g.Add(func() error {
			c := make(chan os.Signal, 1)
			signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
			select {
			case sig := <-c:
				return fmt.Errorf("received signal %s", sig)
			case <-cancelInterrupt:
				return nil
			}
		}, func(error) {
			close(cancelInterrupt)
		})
	}
	if err := g.Run(); err != nil {
		log.Fatal(err)
	}
}

type userServer struct {
	conn      *sql.DB
	tableName string
}

func NewUserGRPCServer(conn *sql.DB, tableName string) apiv1.UserServiceServer {
	return &userServer{
		conn:      conn,
		tableName: tableName,
	}
}

func (s *userServer) CreateUser(ctx context.Context, req *apiv1.CreateUserRequest) (*apiv1.CreateUserResponse, error) {
	if req.User == nil {
		return nil,
			status.Error(codes.InvalidArgument, "User required")
	}
	id, err := uuid.NewRandom()
	if err != nil {
		return nil,
			status.Error(codes.Internal, err.Error())
	}
	query := `INSERT INTO ` + s.tableName + `(id, name, type) VALUES(?,?,?)`
	_, err = runWriteTransaction(s.conn, query, id, req.User.Name, req.User.Type)
	if err != nil {
		return nil, err
	}
	return &apiv1.CreateUserResponse{
		Id: id.String(),
	}, nil
}

func (s *userServer) GetUsers(ctx context.Context, req *apiv1.GetUsersRequest) (*apiv1.GetUsersResponse, error) {
	query := `SELECT id, name, type FROM ` + s.tableName + ``
	users := []*apiv1.UserRead{}
	err := runQuery(s.conn, query, []interface{}{}, func(rows *sql.Rows) error {
		found := apiv1.UserRead{}
		err := rows.Scan(
			&found.Id,
			&found.Name,
			&found.Type,
		)
		if err != nil {
			return err
		}
		users = append(users, &found)
		return nil
	})
	if err != nil {
		return nil,
			status.Error(codes.NotFound, fmt.Errorf("User not found, err %v", err).Error())
	}
	return &apiv1.GetUsersResponse{
		Users: users,
	}, nil
}

// SQLDatabase is the interface that provides sql methods.
type SQLDatabase interface {
	GetConnection() *sql.DB
	CloseConnection() error
}

type db struct {
	conn *sql.DB
}

// NewDatabase creates a new sql database connection with the base migration setup.
func NewDatabase(driver, database, username, password, host string, port string) (SQLDatabase, error) {
	source := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s", username, password, host, port, database)
	sqlconn, err := sql.Open(driver, source)
	if err != nil {
		return nil, fmt.Errorf("Failed to open database connection, err:%v", err)
	}
	return &db{
		conn: sqlconn,
	}, nil
}

func (h *db) GetConnection() *sql.DB {
	return h.conn
}

func (h *db) CloseConnection() error {
	if h.conn == nil {
		return errors.New("Cannot close the connection because the connection is nil")
	}
	return h.conn.Close()
}

func runWriteTransaction(db *sql.DB, query string, args ...interface{}) (sql.Result, error) {
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tx.Rollback() // The rollback will be ignored if the tx has been committed later in the function.
	}()
	stmt, err := tx.Prepare(query)
	if err != nil {
		return nil, err
	}
	defer stmt.Close() // Prepared statements take up server resources and should be closed after use.

	queryResult, err := stmt.Exec(args...)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return queryResult, err
}

func runQuery(db *sql.DB, query string, args []interface{}, f func(*sql.Rows) error) error {
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	queryResult, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() {
		_ = queryResult.Close()
	}()
	for queryResult.Next() {
		err = f(queryResult)
		if err != nil {
			return err
		}
	}
	return nil
}
