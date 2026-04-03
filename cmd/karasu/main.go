package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"karasu/internal/api"
	"karasu/internal/db"
	"karasu/internal/library"
	"karasu/internal/metadata"
	"karasu/internal/slskd"
)

func main() {
	// Load .env file if it exists
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	fmt.Println("Karasu starting...")

	// Build the database connection string from environment variables
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		getEnv("DB_HOST", "localhost"),
		getEnv("DB_PORT", "5432"),
		getEnv("DB_USER", "neosgw"),
		getEnv("DB_PASSWORD", ""),
		getEnv("DB_NAME", "karasu"),
	)

	// Connect to PostgreSQL and run migrations
	database, err := db.Connect(dsn)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer database.Close()

	slskdClient := slskd.NewClient(
		getEnv("SLSKD_URL", "http://localhost:5030"),
		getEnv("SLSKD_USERNAME", "neosgw"),
		getEnv("SLSKD_PASSWORD", ""),
	)
	organizer := library.NewOrganizer(getEnv("MUSIC_DIR", "/mnt/music"))
	downloader := library.NewDownloader(database, slskdClient, organizer)

	mb := metadata.NewMusicBrainzClient()
	fanart := metadata.NewFanartClient(getEnv("FANART_API_KEY", ""))

	// Start the monitoring loop — checks for new releases every 24 hours
	monitor := library.NewMonitor(database, mb, downloader, 24*time.Hour)
	monitor.Start()

	h := api.NewHandler(database, mb, fanart, downloader)

	// Set up the HTTP router
	r := gin.Default()

	// Trust only localhost proxy
	r.SetTrustedProxies([]string{"127.0.0.1"})

	h.RegisterRoutes(r)

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status": "ok",
			"app":    "karasu",
		})
	})

	port := getEnv("PORT", "8080")
	log.Printf("Karasu listening on :%s", port)
	log.Fatal(r.Run(":" + port))
}

// getEnv returns the value of an environment variable or a fallback default
func getEnv(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	return fallback
}
