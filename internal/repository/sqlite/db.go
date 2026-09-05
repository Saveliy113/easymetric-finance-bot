package db

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

var DB *sql.DB

func Init() {
	var err error
	DB, err = sql.Open("sqlite3", "em.db")
	if err != nil {
		panic("Could not connect to the database.")
	}

	if err = DB.Ping(); err != nil {
		panic("Could not ping the database: " + err.Error())
	}

	DB.SetMaxOpenConns(20)
	DB.SetMaxIdleConns(5)

	createTables()
	fmt.Println("⛁ Database conected")
}

func createTables() {
	createUsersTable := `
		CREATE TABLE IF NOT EXISTS users (
			id 				 INTEGER PRIMARY KEY AUTOINCREMENT,
			telegram_id 	 INTEGER NOT NULL UNIQUE,
    		spreadsheet_id   TEXT,
			username         TEXT,
    		state            TEXT NOT NULL DEFAULT 'AWAITING_CITY',
			timezone         TEXT,
			currency		 TEXT,
			categories_cache TEXT,
			created_at 		 DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_users_spreadsheet_id ON users(spreadsheet_id);
	`

	_, err := DB.Exec(createUsersTable)
	if err != nil {
		panic("[ db ] Could not initialize users table: " + err.Error())
	}
}
