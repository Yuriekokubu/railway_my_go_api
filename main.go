package main

import (
    "context"
    "fmt"
    "net/http"
    "os"

    "github.com/gin-gonic/gin"
    "github.com/go-redis/redis/v8"
    "github.com/jackc/pgx/v4" //  เติม github.com/ เข้าไปแบบนี้ครับ
)

var ctx = context.Background()

func main() {
	r := gin.Default()

	// 1. เชื่อมต่อ PostgreSQL
	// Railway จะให้ DATABASE_URL มาโดยอัตโนมัติ
	dbURL := os.Getenv("DATABASE_URL")
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to connect to database: %v\n", err)
	} else {
		defer conn.Close(ctx)
		fmt.Println("Connected to PostgreSQL successfully!")
	}

	// 2. เชื่อมต่อ Redis
	// Railway จะให้ REDIS_URL มาโดยอัตโนมัติ
	redisURL := os.Getenv("REDIS_URL")
	opt, err := redis.ParseURL(redisURL)
	var rdb *redis.Client
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to parse Redis URL: %v\n", err)
	} else {
		rdb = redis.NewClient(opt)
		fmt.Println("Connected to Redis successfully!")
	}

	// API Endpoint
	r.GET("/ping", func(c *gin.Context) {
		// ทดลองเซ็ตค่าใน Redis
		if rdb != nil {
			rdb.Set(ctx, "status", "active", 0)
		}
		c.JSON(http.StatusOK, gin.H{
			"message": "pong",
			"db_status": err == nil,
		})
	})

	// Railway จะกำหนด Port ให้ผ่าน Environment Variable ชื่อ PORT
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	r.Run(":" + port)
}