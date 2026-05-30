# Stage 1: Build the Go binary
FROM golang:1.25-alpine AS builder
WORKDIR /app
# คัดลอกไฟล์โมดูลและดาวน์โหลด Dependencies
COPY go.mod go.sum ./
RUN go mod download
# คัดลอกซอร์สโค้ดทั้งหมด (รวมถึง index.html ด้วย) เข้าไปใน Stage Build
COPY . .
# สั่งคอมไพล์โปรเจกต์ Go ออกมาเป็นไฟล์ Binary ชื่อ main
RUN CGO_ENABLED=0 GOOS=linux go build -o main .

# Stage 2: Run the binary (ยานพาหนะตัวจริงที่ขึ้นไปวิ่งบนคลาวด์)
FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
# 1. ดึงไฟล์คอมไพล์ Binary จาก Stage แรกมาวาง
COPY --from=builder /app/main .
# 2. 🛠️ ดึงไฟล์หน้าเว็บ index.html ข้ามมาวางในฝั่งรันด้วย เพื่อไม่ให้เจอ 404
COPY --from=builder /app/index.html .
# ปรับ EXPOSE พอร์ตให้ยืดหยุ่นรองรับตามที่ Render กำหนด (พอร์ตหลักเป็น 10000 ตามโค้ด Go)
EXPOSE 10000
# สั่งรันแอปพลิเคชัน
CMD ["./main"]