package main

import (
	"database/sql"
	_ "embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/libsql/libsql-client-go/libsql"
	"github.com/spf13/cobra"
)

//go:embed sql/create_migrations_table.sql
var createMigrationsTableSQL string

//go:embed sql/check_migrations_table.sql
var checkMigrationsTableSQL string

//go:embed sql/check_migration.sql
var checkMigrationSQL string

//go:embed sql/record_migration.sql
var recordMigrationSQL string

//go:embed sql/delete_migration.sql
var deleteMigrationSQL string

var migrationsPath string
var dbURL string
var migrationsTableName string

const DEFAULT_MIGRATIONS_TABLE = "libsql_migrations"

func main() {
	var rootCmd = &cobra.Command{
		Use:   "libsql-migrate",
		Short: "A CLI tool for generating migration files",
	}

    var versionCmd = &cobra.Command{
        Use: "version",
        Short: "Get version of libsql-migrate CLI",
        Run: version,
    }

	var generateCmd = &cobra.Command{
		Use:   "gen <name>",
		Short: "Genereate new migration files",
		Args:  cobra.ExactArgs(1),
		Run:   generate,
	}
	generateCmd.Flags().StringVarP(&migrationsPath, "path", "p", "./", "Path to store the migration files")

	var upCmd = &cobra.Command{
		Use:   "up",
		Short: "Performs migrations",
		Args:  cobra.NoArgs,
		Run:   up,
	}
	upCmd.Flags().StringVarP(&migrationsPath, "path", "p", "./", "Path where the migration files are located")
	upCmd.Flags().StringVarP(&dbURL, "url", "u", "", "The Database URL. If env DB_URL is defined then this is not needed")
    upCmd.Flags().StringVarP(&migrationsTableName, "table", "t", DEFAULT_MIGRATIONS_TABLE,  "The migrations table name.")
	upCmd.MarkFlagRequired("url")
	upCmd.MarkFlagRequired("path")

	var downCmd = &cobra.Command{
		Use:   "down",
		Short: "Rollbacks migrations",
		Args:  cobra.NoArgs,
		Run:   down,
	}
	downCmd.Flags().StringVarP(&migrationsPath, "path", "p", "./", "Path where the migration files are located")
	downCmd.Flags().StringVarP(&dbURL, "url", "u", "", "The Database URL. If env DB_URL is defined then this is not needed")
    downCmd.Flags().StringVarP(&migrationsTableName, "table", "t", DEFAULT_MIGRATIONS_TABLE,  "The migrations table name.")
	downCmd.MarkFlagRequired("url")
	downCmd.MarkFlagRequired("path")

	rootCmd.AddCommand(generateCmd)
	rootCmd.AddCommand(upCmd)
	rootCmd.AddCommand(downCmd)
    rootCmd.AddCommand(versionCmd)

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
		os.Exit(1)
	}

}

func connect() *sql.DB {
	db, err := sql.Open("libsql", dbURL)
	if err != nil {
		log.Fatalf("Error opening a connection to database: %v", err)
		os.Exit(1)
	}
	return db
}

func up(cmd *cobra.Command, args []string) {
	db := connect()
	defer db.Close()

	err := checkMigrationTable(db)
	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}

	files, err := os.ReadDir(migrationsPath)
	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}

	sort.Slice(files, func(i, j int) bool {
		// ascending order
		return files[i].Name() < files[j].Name()
	})

	var suffix = "_up.sql"
	for _, file := range files {
		if strings.HasSuffix(file.Name(), suffix) {
			baseName := strings.TrimSuffix(file.Name(), suffix)
			var id int32
			err := db.QueryRow(prepareSQL(checkMigrationSQL), baseName).Scan(&id)
			if err != nil && err != sql.ErrNoRows {
				log.Fatalf("Failed to query migrations table: %v", err)
				os.Exit(1)
			}

			if err == sql.ErrNoRows {
				fmt.Printf("Applying migration: %s\n", file.Name())

				// apply migrations
				content, err := os.ReadFile(filepath.Join(migrationsPath, file.Name()))
				if err != nil {
					log.Fatalf("Failed to apply migration from file %s: %v", file.Name(), err)
					os.Exit(1)
				}

				_, err = db.Exec(string(content))
				if err != nil {
					log.Fatalf("Failed to apply migration from file %s: %v", file.Name(), err)
					os.Exit(1)
				}

				_, err = db.Exec(prepareSQL(recordMigrationSQL), baseName)
				if err != nil {
					log.Fatalf("Failed to insert migration record into migrations table: %v", err)
					os.Exit(1)
				}
			}
		}
	}

	fmt.Println("Finished applying migrations ✅")
}

func down(cmd *cobra.Command, args []string) {
	db := connect()
	defer db.Close()

	err := checkMigrationTable(db)
	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}

	files, err := os.ReadDir(migrationsPath)
	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}

	sort.Slice(files, func(i, j int) bool {
		// descending order
		return files[i].Name() > files[j].Name()
	})

	suffix := "_down.sql"
	for _, file := range files {
		if strings.HasSuffix(file.Name(), suffix) {
			baseName := strings.TrimSuffix(file.Name(), suffix)
			var id int32
			err := db.QueryRow(prepareSQL(checkMigrationSQL), baseName).Scan(&id)
			if err != nil && err != sql.ErrNoRows {
				log.Fatalf("Failed to query migrations table: %v", err)
				os.Exit(1)
			}

			if err == nil && id > 0 {
				fmt.Printf("Resetting migration: %s\n", file.Name())

				// apply migrations
				content, err := os.ReadFile(filepath.Join(migrationsPath, file.Name()))
				if err != nil {
					log.Fatalf("Failed to apply migration from file %s: %v", file.Name(), err)
					os.Exit(1)
				}

				_, err = db.Exec(string(content))
				if err != nil {
					log.Fatalf("Failed to apply migration from file %s: %v", file.Name(), err)
					os.Exit(1)
				}
				_, err = db.Exec(prepareSQL(deleteMigrationSQL), id)
				if err != nil {
					log.Fatalf("Failed to remove migration record from migrations table: %v", err)
					os.Exit(1)
				}
			}
		}
	}

	fmt.Println("Finished rolling back migrations ✅")
}

func generate(cmd *cobra.Command, args []string) {
	migrationName := args[0]
	timestamp := time.Now().UTC().Format("20060102150405")

	upMigrationFile := fmt.Sprintf("%s_%s_up.sql", timestamp, migrationName)
	downMigrationFile := fmt.Sprintf("%s_%s_down.sql", timestamp, migrationName)
	upMigration := filepath.Join(migrationsPath, upMigrationFile)
	downMigration := filepath.Join(migrationsPath, downMigrationFile)

	upFile, err := os.Create(upMigration)
	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}
	defer upFile.Close()

	_, err = upFile.WriteString("-- Write your UP migration SQL here.\n")
	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}

	downFile, err := os.Create(downMigration)
	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}
	defer downFile.Close()

	_, err = downFile.WriteString("-- Write your DOWN migration SQL here.\n")
	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}

	fmt.Println("Migration files:")
	fmt.Printf("Up migration: %s\n", upMigration)
	fmt.Printf("Down migration: %s\n", downMigration)
}

func checkMigrationTable(db *sql.DB) error {
	// check if migrations table exists or not
	rows, err := db.Exec(checkMigrationsTableSQL, migrationsTableName)
	if err != nil {
		return fmt.Errorf("Failed to check migration table: %v", err)
	}

	count, err := rows.RowsAffected()
	if err != nil {
		return fmt.Errorf("Failed to get rows affected count when checking migration table: %v", err)
	}

	if count == 0 {
		// table does not exists, create table
        sql := prepareSQL(createMigrationsTableSQL)
		_, err = db.Exec(sql)
		if err != nil {
            return fmt.Errorf("Failed to create migration table: %v", err)
		}
	}

	return nil
}

func prepareSQL(sql string) string {
    return fmt.Sprintf(sql, migrationsTableName)
}

func version(cmd *cobra.Command, args []string) {
    fmt.Println("Running libsql-migrate v0.1.0")
}
