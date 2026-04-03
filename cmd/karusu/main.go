package main

import (
	"fmt"
	"log"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"karusu/internal/db"
)

func main() {
	// Load .env file if it exists
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	fmt.Println("Karusu starting...")

	// Build the database connection string from environment variables
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		getEnv("DB_HOST", "localhost"),
		getEnv("DB_PORT", "5432"),
		getEnv("DB_USER", "neosgw"),
		getEnv("DB_PASSWORD", ""),
		getEnv("DB_NAME", "karusu"),
	)

	// Connect to PostgreSQL and run migrations
	database, err := db.Connect(dsn)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer database.Close()

	// Set up the HTTP router
	r := gin.Default()

	// Trust only localhost proxy
	r.SetTrustedProxies([]string{"127.0.0.1"})

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status": "ok",
			"app":    "karusu",
		})
	})

	port := getEnv("PORT", "8080")
	log.Printf("Karusu listening on :%s", port)
	log.Fatal(r.Run(":" + port))
}

// getEnv returns the value of an environment variable or a fallback default
func getEnv(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	return fallback
}
