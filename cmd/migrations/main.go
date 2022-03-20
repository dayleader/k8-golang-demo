package main

import (
	"fmt"
	"os"

	"database/sql"

	"github.com/cenk/backoff"
	_ "github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

const migrationsPath = "file://migrations"

func main() {

	// Open connection.
	conn, driver := getConnection()

	// Close connection.
	defer func() {
		if conn != nil {
			_ = conn.Close()
		}
	}()

	// Establish connection to a database.
	establishConnectionWithRetry(conn)

	// Starting migration job.
	err := migrateSQL(conn, driver)
	if err != nil {
		panic(err)
	} else {
		fmt.Println("Migration successfully finished.")
	}
}

func getConnection() (*sql.DB, string) {
	driver := os.Getenv("MYSQL_DRIVER")
	address := os.Getenv("MYSQL_HOST") + ":" + os.Getenv("MYSQL_PORT_NUMBER")
	username := os.Getenv("MYSQL_DATABASE_USER")
	password := os.Getenv("MYSQL_DATABASE_PASSWORD")
	database := os.Getenv("MYSQL_DATABASE_NAME")

	// Open may just validate its arguments without creating a connection to the database.
	sqlconn, err := sql.Open(driver, username+":"+password+"@tcp("+address+")/"+database)
	if err != nil {
		panic("Cannot establish connection to a database")
	}

	return sqlconn, driver
}

// This function executes the migration scripts.
func migrateSQL(conn *sql.DB, driverName string) error {
	driver, _ := mysql.WithInstance(conn, &mysql.Config{})
	m, err := migrate.NewWithDatabaseInstance(
		migrationsPath,
		driverName,
		driver,
	)
	if err != nil {
		return err
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return err
	}
	return nil
}

func establishConnectionWithRetry(conn *sql.DB) {
	b := backoff.NewExponentialBackOff()
	// We wait forever until the connection will be established.
	// In practice k8s will kill the pod if it takes too long.
	b.MaxElapsedTime = 0

	_ = backoff.Retry(func() error {
		fmt.Println("Connecting to a database ...")
		// Ping verifies a connection to the database is still alive,
		// establishing a connection if necessary.
		if errPing := conn.Ping(); errPing != nil {
			return fmt.Errorf("ping failed %v", errPing)
		}
		return nil
	}, b)
}
