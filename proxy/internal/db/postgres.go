package db

import (
	"database/sql"
	"log"
	"os"

	_ "github.com/lib/pq"
)

var dbClient *sql.DB

// InitPostgres connects to PostgreSQL and returns the client
func InitPostgres() {
	dbUrl := os.Getenv("DATABASE_URL")
	if dbUrl == "" {
		dbUrl = "postgresql://user:password@localhost:5432/optitoken_db?sslmode=disable"
		log.Println("WARNING: DATABASE_URL is not set. Using default local credentials. Do not use this in production!")
	}

	var err error
	dbClient, err = sql.Open("postgres", dbUrl)
	if err != nil {
		log.Fatalf("Failed to connect to postgres: %v", err)
	}
}

// GetDB returns the active Postgres connection pool
func GetDB() *sql.DB {
	return dbClient
}
