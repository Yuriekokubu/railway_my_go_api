package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/jackc/pgx/v4"
)

// โครงสร้างข้อมูล Product
type Product struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`  // เติม string ตรงนี้ครับ
	Price float64 `json:"price"`
}

var (
	ctx       = context.Background()
	dbConn    *pgx.Conn
	rdbClient *redis.Client
)

func main() {
	r := gin.Default()

	// 1. เชื่อมต่อ PostgreSQL
	var err error
	dbConn, err = pgx.Connect(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to connect to database: %v\n", err)
		os.Exit(1)
	}
	defer dbConn.Close(ctx)
	fmt.Println("Connected to PostgreSQL successfully!")

	// สั่งสร้าง Table อัตโนมัติถ้ายังไม่มี
	createTableSQL := `CREATE TABLE IF NOT EXISTS products (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		price NUMERIC NOT NULL
	);`
	_, _ = dbConn.Exec(ctx, createTableSQL)

	// 2. เชื่อมต่อ Redis
	opt, err := redis.ParseURL(os.Getenv("REDIS_URL"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to parse Redis URL: %v\n", err)
		os.Exit(1)
	}
	rdbClient = redis.NewClient(opt)
	if err := rdbClient.Ping(ctx).Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Unable to connect to Redis: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Connected to Redis successfully!")

	// --- ROUTES ZONE ---

	// Test Routeเดิม
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"db_status": true, "message": "pong"})
	})

	// CRUD Routes
	r.POST("/products", createProduct)          // Create
	r.GET("/products/:id", getProductWithCache) // Read (มีระบบ Redis Cache)
	r.PUT("/products/:id", updateProduct)       // Update
	r.DELETE("/products/:id", deleteProduct)    // Delete

	// เริ่มรัน server บนพอร์ตที่ Render กำหนด
	port := os.Getenv("PORT")
	if port == "" {
		port = "10000"
	}
	r.Run(":" + port)
}

// ==========================================
// CONTROLLER & LOGIC FUNCTIONS
// ==========================================

// 1. CREATE: เพิ่มสินค้าใหม่
func createProduct(c *gin.Context) {
	var p Product
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	_, err := dbConn.Exec(ctx, "INSERT INTO products (id, name, price) VALUES ($1, $2, $3)", p.ID, p.Name, p.Price)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create product"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Product created successfully", "data": p})
}

// 2. READ: ดึงข้อมูลสินค้า (มีระบบตรวจและเซ็ต Redis Cache ในตัว)
func getProductWithCache(c *gin.Context) {
	id := c.Param("id")
	redisKey := "product:" + id

	// [STEP A] ตรวจสอบว่ามีข้อมูลใน Redis Cache ไหม
	cachedData, err := rdbClient.Get(ctx, redisKey).Result()
	if err == nil {
		// เจอในแคช (Cache Hit!) -> แปลงข้อความกลับเป็น JSON แล้วส่งได้ทันทีแบบติดสปีด
		var p Product
		_ = json.Unmarshal([]byte(cachedData), &p)
		c.JSON(http.StatusOK, gin.H{"source": "redis_cache", "data": p})
		return
	}

	// [STEP B] ถ้าไม่เจอในแคช (Cache Miss!) -> วิ่งไปค้นใน PostgreSQL แทน
	var p Product
	err = dbConn.QueryRow(ctx, "SELECT id, name, price FROM products WHERE id=$1", id).Scan(&p.ID, &p.Name, &p.Price)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Product not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		}
		return
	}

	// [STEP C] เจอข้อมูลใน DB -> เซ็ตลง Redis Cache เก็บไว้ใช้งานใน 60 วินาทีถัดไปก่อนส่งกลับ
	pBytes, _ := json.Marshal(p)
	_ = rdbClient.Set(ctx, redisKey, pBytes, 60*time.Second).Err()

	c.JSON(http.StatusOK, gin.H{"source": "postgresql_db", "data": p})
}

// 3. UPDATE: แก้ไขข้อมูลสินค้า
func updateProduct(c *gin.Context) {
	id := c.Param("id")
	var p Product
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	_, err := dbConn.Exec(ctx, "UPDATE products SET name=$1, price=$2 WHERE id=$3", p.Name, p.Price, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update product"})
		return
	}

	// เคลียร์แคชทิ้งทันทีเมื่อข้อมูลอัปเดต เพื่อป้องกันข้อมูลขัดแย้ง
	_ = rdbClient.Del(ctx, "product:"+id)

	c.JSON(http.StatusOK, gin.H{"message": "Product updated successfully"})
}

// 4. DELETE: ลบสินค้า
func deleteProduct(c *gin.Context) {
	id := c.Param("id")

	_, err := dbConn.Exec(ctx, "DELETE FROM products WHERE id=$1", id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete product"})
		return
	}

	// เคลียร์แคชทิ้งทันทีเมื่อข้อมูลโดนลบ
	_ = rdbClient.Del(ctx, "product:"+id)

	c.JSON(http.StatusOK, gin.H{"message": "Product deleted successfully"})
}

// 5. READ ALL: ดึงข้อมูลสินค้าทั้งหมด (มีระบบตรวจและเซ็ต Redis Cache สำหรับ List)
func getAllProductsWithCache(c *gin.Context) {
	redisKey := "products:all"

	// [STEP A] ตรวจสอบแคชใน Redis
	cachedData, err := rdbClient.Get(ctx, redisKey).Result()
	if err == nil {
		var products []Product
		_ = json.Unmarshal([]byte(cachedData), &products)
		c.JSON(http.StatusOK, gin.H{"source": "redis_cache", "data": products})
		return
	}

	// [STEP B] ถ้าไม่มีแคช -> ไปดึงจาก PostgreSQL
	rows, err := dbConn.Query(ctx, "SELECT id, name, price FROM products")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}
	defer rows.Close()

	products := []Product{}
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.Name, &p.Price); err == nil {
			products = append(products, p)
		}
	}

	// [STEP C] เซ็ตลง Redis Cache เก็บไว้ 60 วินาที
	pBytes, _ := json.Marshal(products)
	_ = rdbClient.Set(ctx, redisKey, pBytes, 60*time.Second).Err()

	c.JSON(http.StatusOK, gin.H{"source": "postgresql_db", "data": products})
}
