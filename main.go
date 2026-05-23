package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/bsm/redislock"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Resource struct {
	ID      string `json:"id"`
	Data    string `json:"data"`
	Version int    `json:"version"`
}

var (
	dbPool      *pgxpool.Pool
	redisClient *redis.Client
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:password@localhost:5432/concur_db?sslmode=disable"
	}
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "localhost:6379"
	}

	ctx := context.Background()

	// 1. ПОДКЛЮЧЕНИЕ К POSTGRESQL С ПОВТОРАМИ (Retry)
	var err error
	for i := 1; i <= 10; i++ { // 10 попыток
		log.Printf("Попытка подключения к PostgreSQL %d/10...\n", i)
		dbPool, err = pgxpool.New(ctx, dbURL)
		if err == nil {
			// Проверяем реальное соединение
			err = dbPool.Ping(ctx)
			if err == nil {
				log.Println("Успешно подключено к PostgreSQL!")
				break
			}
		}
		log.Printf("Ожидание базы данных: %v. Повтор через 2 секунды...\n", err)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		log.Fatalf("Не удалось подключиться к БД после 10 попыток: %v\n", err)
	}
	defer dbPool.Close()
	initDatabase(ctx)

	// 2. ПОДКЛЮЧЕНИЕ К REDIS С ПОВТОРАМИ (Retry)
	redisClient = redis.NewClient(&redis.Options{Addr: redisURL})
	for i := 1; i <= 10; i++ {
		log.Printf("Попытка подключения к Redis %d/10...\n", i)
		err = redisClient.Ping(ctx).Err()
		if err == nil {
			log.Println("Успешно подключено к Redis!")
			break
		}
		log.Printf("Ожидание Redis: %v. Повтор через 2 секунды...\n", err)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		log.Printf("Предупреждение: Redis окончательно недоступен: %v\n", err)
	}

	// Регистрируем хендлеры для проверки готовности Kubernetes (Healthchecks)
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	http.HandleFunc("/resource/optimistic", handleOptimistic)
	http.HandleFunc("/resource/pessimistic", handlePessimistic)
	http.HandleFunc("/resource/redis", handleRedisLock)

	fmt.Println("Сервер concur-res запущен на :8080 с 3-мя видами конкурентности!")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func initDatabase(ctx context.Context) {
	query := `
	CREATE TABLE IF NOT EXISTS resources (
		id TEXT PRIMARY KEY,
		data TEXT NOT NULL,
		version INT NOT NULL
	);
	INSERT INTO resources (id, data, version) VALUES ('1', 'Старт', 1) ON CONFLICT DO NOTHING;
	`
	_, err := dbPool.Exec(ctx, query)
	if err != nil {
		log.Fatalf("Ошибка инициализации БД: %v", err)
	}
}

// === 1. ОПТИМИСТИЧЕСКАЯ БЛОКИРОВКА ===
func handleOptimistic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Only PUT allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	var input Resource
	json.NewDecoder(r.Body).Decode(&input)

	query := `UPDATE resources SET data = $1, version = version + 1 WHERE id = '1' AND version = $2 RETURNING version;`
	var newVersion int
	err := dbPool.QueryRow(r.Context(), query, input.Data, input.Version).Scan(&newVersion)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || err.Error() == "no rows in result set" {
			http.Error(w, "Конфликт: ресурс изменен другим пользователем", http.StatusConflict)
			return
		}
		http.Error(w, "Ошибка БД", 500)
		return
	}
	input.ID = "1"
	input.Version = newVersion
	json.NewEncoder(w).Encode(input)
}

// === 2. ПЕССИМИСТИЧЕСКАЯ БЛОКИРОВКА ===
func handlePessimistic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Only PUT allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	var input Resource
	json.NewDecoder(r.Body).Decode(&input)

	ctx := r.Context()
	tx, err := dbPool.Begin(ctx)
	if err != nil {
		http.Error(w, "Tx error", 500)
		return
	}
	defer tx.Rollback(ctx)

	var current Resource
	tx.QueryRow(ctx, "SELECT id, data, version FROM resources WHERE id='1' FOR UPDATE").Scan(&current.ID, &current.Data, &current.Version)

	time.Sleep(3 * time.Second) // Имитируем задержку для проверки очереди

	newVersion := current.Version + 1
	tx.Exec(ctx, "UPDATE resources SET data = $1, version = $2 WHERE id = '1'", input.Data, newVersion)
	tx.Commit(ctx)

	input.ID = "1"
	input.Version = newVersion
	json.NewEncoder(w).Encode(input)
}

// === 3. РАСПРЕДЕЛЕННАЯ БЛОКИРОВКА ЧЕРЕЗ REDIS (Дополнение) ===
func handleRedisLock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Only PUT allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	var input Resource
	json.NewDecoder(r.Body).Decode(&input)

	ctx := r.Context()
	// Создаем менеджер блокировок Redis
	locker := redislock.New(redisClient)

	log.Println("--> Пробуем занять ключ в Redis...")
	// Пытаемся захватить ключ на 4 секунды. Если занят — отдаем статус 423 (Locked)
	lock, err := locker.Obtain(ctx, "lock:resource:1", 4*time.Second, nil)
	if err == redislock.ErrNotObtained {
		http.Error(w, "Ресурс заблокирован в Redis, попробуйте позже", http.StatusLocked)
		return
	} else if err != nil {
		http.Error(w, "Ошибка Redis-локера", 500)
		return
	}
	defer lock.Release(ctx) // Гарантированно освобождаем ключ в Redis при выходе

	log.Println("Ключ в Redis успешно захвачен! Работаем с БД...")
	time.Sleep(3 * time.Second) // Имитируем работу

	// Безопасно обновляем БД (блокировки на уровне БД не нужны, Redis нас защищает)
	var currentVersion int
	dbPool.QueryRow(ctx, "SELECT version FROM resources WHERE id='1'").Scan(&currentVersion)
	newVersion := currentVersion + 1
	dbPool.Exec(ctx, "UPDATE resources SET data = $1, version = $2 WHERE id = '1'", input.Data, newVersion)

	log.Println("<-- Работа завершена, отпускаем Redis")
	input.ID = "1"
	input.Version = newVersion
	json.NewEncoder(w).Encode(input)
}
