package main

import (
	"database/sql"
	"errors"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

const databaseFile string = "log.sqlite"

const createConnectionsTable string = `
CREATE TABLE IF NOT EXISTS connections (
	id integer PRIMARY KEY AUTOINCREMENT,
    time TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	source_ip TEXT NOT NULL,
	source_port integer NOT NULL,
	continent TEXT NOT NULL,
	country TEXT NOT NULL,
	city TEXT NOT NULL,
	attempts integer,
	container TEXT NOT NULL
);`

const createMetadataTable string = `
CREATE TABLE IF NOT EXISTS metadata (
	id integer NOT NULL,
    delay integer NOT NULL,
  	net_data BLOB
);
`

const createPasswordsAttemptsTable string = `
CREATE TABLE IF NOT EXISTS attempts (
	id integer NOT NULL,
	username TEXT,
	password TEXT
);`

const insertConnection string = `
INSERT INTO connections (
	source_ip,
	source_port,
	continent,
	country,
	city,
	attempts,
	container
) VALUES (?, ?, ?, ?, ?, ?, ?)`

const insertAttempt string = `
INSERT INTO attempts (
	id,
	username,
	password
) VALUES (?, ?, ?)`

const insertMetadata string = `
INSERT INTO metadata (
	id,
	delay,
	net_data
) VALUES (?, ?, ?)
`

// SQLHoneypotDBConnection defines the connection for the database
type SQLHoneypotDBConnection struct {
	database *sql.DB
	ConnID   uint32
}

// NewSQLHoneypotDBConnection creates a new DB connection for one client
func NewSQLHoneypotDBConnection(sourceIP string, sourcePort uint16, geoData GeoData, pwdData PasswordAttemptData, container string) SQLHoneypotDBConnection {
	connection := SQLHoneypotDBConnection{
		database: nil,
		ConnID:   0,
	}

	err := connection.initDatabaseConnection()

	if err != nil {
		debugPrint(fmt.Sprintf("The database handler has encountered an unrecoverable error: %s", err))
		return connection
	}

	// Add to the connections table
	err = connection.insertInitialConnection(sourceIP, sourcePort, geoData, pwdData, container)

	if err != nil {
		debugPrint(fmt.Sprintf("Unable to insert initial connection: %s", err))
		return connection
	}

	return connection
}

func (sq *SQLHoneypotDBConnection) initDatabaseConnection() error {
	database, err := sql.Open("sqlite3", databaseFile)

	// An error has occurred
	if err != nil {
		return err
	}

	// Set database attribute
	sq.database = database

	err = sq.createTablesIfNotExists()

	if err != nil {
		return err
	}

	return nil
}

func (sq *SQLHoneypotDBConnection) createTablesIfNotExists() error {
	// Create the connections table
	statement, err := sq.database.Prepare(createConnectionsTable)

	// An error has occurred
	if err != nil {
		return err
	}

	// Execute statement
	_, err = statement.Exec()

	// An error has occurred
	if err != nil {
		return err
	}

	// Create the connections table
	statement, err = sq.database.Prepare(createMetadataTable)

	// An error has occurred
	if err != nil {
		return err
	}

	// Execute statement
	_, err = statement.Exec()

	// An error has occurred
	if err != nil {
		return err
	}

	// Create the attempts table
	statement, err = sq.database.Prepare(createPasswordsAttemptsTable)

	// An error has occurred
	if err != nil {
		return err
	}

	// Execute statement
	_, err = statement.Exec()

	// An error has occurred
	if err != nil {
		return err
	}

	return nil
}

// Close the database
func (sq *SQLHoneypotDBConnection) Close() error {
	if sq.database == nil {
		return nil
	}

	return sq.database.Close()
}

// InsertMetadata inserts metadata to the respective connection
func (sq *SQLHoneypotDBConnection) InsertMetadata(bytes []byte, delay int64) error {
	// Check for failures
	if sq.database == nil {
		return errors.New("database does not exist")
	}

	if sq.ConnID == 0 {
		return errors.New("connection id does not exist")
	}

	statement, err := sq.database.Prepare(insertMetadata)

	if err != nil {
		return err
	}

	_, err = statement.Exec(sq.ConnID, delay, bytes)

	if err != nil {
		return err
	}

	return nil
}

func (sq *SQLHoneypotDBConnection) insertInitialConnection(sourceIP string, sourcePort uint16, geoData GeoData, pwdData PasswordAttemptData, container string) error {
	if sq.database == nil {
		return errors.New("database does not exist")
	}

	statement, err := sq.database.Prepare(insertConnection)

	if err != nil {
		return err
	}

	_, err = statement.Exec(sourceIP, sourcePort, geoData.ContinentCode, geoData.CountryCode, geoData.City, pwdData.numAttempts, container)

	if err != nil {
		return err
	}

	// Get the last inserted rowid
	rows, err := sq.database.Query("SELECT last_insert_rowid()")

	if err != nil {
		return err
	}

	defer rows.Close()

	rows.Next()
	err = rows.Scan(&sq.ConnID)

	if err != nil {
		return err
	}

	// Finally insert the password attempt data
	for _, elem := range pwdData.usernamePasswords {
		statement, err = sq.database.Prepare(insertAttempt)

		if err != nil {
			return err
		}

		_, err = statement.Exec(sq.ConnID, elem.username, elem.password)

		if err != nil {
			return err
		}
	}

	return nil
}

// GetContainerIDFromConnection gets the container ID from a specific IP
func GetContainerIDFromConnection(ip string) (string, error) {
	// Create new connection, just for this transaction
	connection := SQLHoneypotDBConnection{
		database: nil,
		ConnID:   0,
	}

	err := connection.initDatabaseConnection()

	if err != nil {
		debugPrint(fmt.Sprintf("The database handler has encountered an unrecoverable error: %s", err))
		return "", err
	}

	// Close connection when done
	defer connection.Close()

	rows, err := connection.database.Query("SELECT container FROM connections WHERE source_ip == ? ORDER BY id DESC", ip)

	defer rows.Close()

	if err != nil {
		return "", err
	}

	containerID := ""

	nextRowExists := rows.Next()

	// If we have a result, return it
	if nextRowExists {
		err = rows.Scan(&containerID)

		if err != nil {
			return "", err
		}

		return containerID, nil
	}

	// If no result, return empty string
	return "", nil
}
