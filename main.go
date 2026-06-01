package main

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/jackc/pgx/v4"
)

//go:embed index.html
var indexHTML []byte

// โครงสร้างข้อมูล Product
type Product struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Price float64 `json:"price"`
}

var (
	ctx       = context.Background()
	dbConn    *pgx.Conn
	rdbClient *redis.Client
	appEnv    = "production"
)

func main() {
	loadConfig()

	if appEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.Default()

	// 🔓 ปลดล็อก CORS ให้หน้าบ้านเข้าถึง API ได้จากทุกที่
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// 🌐 Serving Static Files (เปิดให้ดึงหน้าเว็บ index.html จากเซิร์ฟเวอร์โดยตรง)
	r.GET("/", serveIndex)
	r.GET("/index.html", serveIndex)

	// 1. เชื่อมต่อ PostgreSQL
	var err error
	dbConn, err = pgx.Connect(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		exitWithError("Unable to connect to database: %v", err)
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
		exitWithError("Unable to parse Redis URL: %v", err)
	}
	rdbClient = redis.NewClient(opt)
	if err := rdbClient.Ping(ctx).Err(); err != nil {
		exitWithError("Unable to connect to Redis: %v", err)
	}
	fmt.Println("Connected to Redis successfully!")

	// --- ROUTES ZONE ---

	// Test Route เดิม
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"db_status": true, "message": "pong"})
	})

	// CRUD Routes
	r.POST("/products", createProduct)          // Create
	r.GET("/products", getAllProductsWithCache) // Read All (มี Redis Cache)
	r.GET("/products/:id", getProductWithCache) // Read One (มี Redis Cache)
	r.PUT("/products/:id", updateProduct)       // Update
	r.DELETE("/products/:id", deleteProduct)    // Delete

	// เริ่มรัน server บนพอร์ตที่ Render กำหนด
	port := os.Getenv("PORT")
	if port == "" {
		port = "10000"
	}
	fmt.Printf("Starting %s server on port %s\n", appEnv, port)
	if err := r.Run(":" + port); err != nil {
		exitWithError("Unable to start server on port %s: %v", port, err)
	}
}

func serveIndex(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML)
}

func loadConfig() {
	if env := strings.TrimSpace(os.Getenv("APP_ENV")); env != "" {
		appEnv = env
	}

	for _, path := range envFilesFor(appEnv) {
		if err := loadEnvFile(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			exitWithError("Unable to load %s: %v", path, err)
		}
	}

	if os.Getenv("DATABASE_URL") == "" {
		exitWithError("DATABASE_URL is empty. Please create .env.dev or .env.production and set DATABASE_URL.")
	}
	if os.Getenv("REDIS_URL") == "" {
		exitWithError("REDIS_URL is empty. Please create .env.dev or .env.production and set REDIS_URL.")
	}
}

func envFilesFor(env string) []string {
	roots := envSearchRoots()
	names := []string{}

	switch strings.ToLower(env) {
	case "dev", "development":
		appEnv = "development"
		names = []string{".env.dev", ".env.development", ".env"}
	case "prod", "production":
		appEnv = "production"
		names = []string{".env.production", ".env.prod", ".env"}
	default:
		names = []string{".env." + env, ".env"}
	}

	files := make([]string, 0, len(roots)*len(names))
	for _, root := range roots {
		for _, name := range names {
			files = append(files, filepath.Join(root, name))
		}
	}
	return files
}

func envSearchRoots() []string {
	roots := []string{"."}

	exePath, err := os.Executable()
	if err != nil {
		return roots
	}

	exeDir := filepath.Dir(exePath)
	parentDir := filepath.Dir(exeDir)

	return append(roots, exeDir, parentDir)
}

func loadEnvFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key != "" && os.Getenv(key) == "" {
			_ = os.Setenv(key, value)
		}
	}

	return scanner.Err()
}

func exitWithError(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Press Enter to exit...")
	_, _ = fmt.Scanln()
	os.Exit(1)
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

	// 🧹 เคลียร์แคชรายการทั้งหมดทิ้ง เพราะมีของใหม่เพิ่มเข้ามาแล้ว
	_ = rdbClient.Del(ctx, "products:all")

	c.JSON(http.StatusCreated, gin.H{"message": "Product created successfully", "data": p})
}

// 2. READ ONE: ดึงข้อมูลสินค้าชิ้นเดียว (มีระบบ Redis Cache)
func getProductWithCache(c *gin.Context) {
	id := c.Param("id")
	redisKey := "product:" + id

	// [STEP A] ตรวจสอบว่ามีข้อมูลใน Redis Cache ไหม
	cachedData, err := rdbClient.Get(ctx, redisKey).Result()
	if err == nil {
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

	// [STEP C] เจอข้อมูลใน DB -> เซ็ตลง Redis Cache เก็บไว้ใช้งานใน 60 วินาที
	pBytes, _ := json.Marshal(p)
	_ = rdbClient.Set(ctx, redisKey, pBytes, 60*time.Second).Err()

	c.JSON(http.StatusOK, gin.H{"source": "postgresql_db", "data": p})
}

// 3. READ ALL: ดึงข้อมูลสินค้าทั้งหมด (มีระบบ Redis Cache สำหรับ List)
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

// 4. UPDATE: แก้ไขข้อมูลสินค้า
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

	// 🧹 ลบแคชเก่าของสินค้าชิ้นนี้ และแคชของรายการทั้งหมด เพื่อให้ข้อมูลอัปเดตตรงกัน
	_ = rdbClient.Del(ctx, "product:"+id)
	_ = rdbClient.Del(ctx, "products:all")

	c.JSON(http.StatusOK, gin.H{"message": "Product updated successfully"})
}

// 5. DELETE: ลบสินค้า
func deleteProduct(c *gin.Context) {
	id := c.Param("id")

	_, err := dbConn.Exec(ctx, "DELETE FROM products WHERE id=$1", id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete product"})
		return
	}

	// 🧹 ลบแคชสินค้าชิ้นนี้ และแคชของรายการทั้งหมดออกทันที
	_ = rdbClient.Del(ctx, "product:"+id)
	_ = rdbClient.Del(ctx, "products:all")

	c.JSON(http.StatusOK, gin.H{"message": "Product deleted successfully"})
}
