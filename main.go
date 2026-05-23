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

	"github.com/jackc/pgx/v5/pgxpool"
)

type Resource struct {
	ID      string `json:"id"`
	Data    string `json:"data"`
	Version int    `json:"version"`
}

// Глобальный пул соединений с базой данных
var dbPool *pgxpool.Pool

func main() {
	// Берем строку подключения из переменной окружения (понадобится для K8s)
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		// Локальный дефолт для тестов
		dbURL = "postgres://postgres:password@localhost:5432/concur_db?sslmode=disable"
	}

	var err error
	ctx := context.Background()

	// Подключаемся к PostgreSQL
	dbPool, err = pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("Не удалось подключиться к БД: %v\n", err)
	}
	defer dbPool.Close()

	// Инициализируем таблицу при старте (для простоты примера)
	initDatabase(ctx)

	http.HandleFunc("/resource", handleResource)

	fmt.Println("Сервер concur-res (с PostgreSQL) запущен на :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func initDatabase(ctx context.Context) {
	// Создаем таблицу, если её нет
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

func handleResource(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	ctx := r.Context()

	id := r.URL.Query().Get("id")
	if id == "" {
		id = "1" // по умолчанию
	}

	switch r.Method {

	case http.MethodGet:
		var res Resource
		// Просто читаем текущие данные из БД
		err := dbPool.QueryRow(ctx, "SELECT id, data, version FROM resources WHERE id=$1", id).
			Scan(&res.ID, &res.Data, &res.Version)

		if err != nil {
			http.Error(w, "Ресурс не найден", http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(res)

	case http.MethodPut:
		var input Resource
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "Ошибка в JSON", http.StatusBadRequest)
			return
		}

		// ГЛАВНЫЙ СЕКРЕТ КОНКУРЕНТНОСТИ:
		// Мы обновляем строку ТОЛЬКО если присланная версия совпадает с версией в БД.
		// В этом же запросе мы инкрементируем версию: version = version + 1
		query := `
			UPDATE resources 
			SET data = $1, version = version + 1 
			WHERE id = $2 AND version = $3
			RETURNING version;
		`

		var newVersion int
		err := dbPool.QueryRow(ctx, query, input.Data, id, input.Version).Scan(&newVersion)

		if err != nil {
			if errors.Is(err, sql.ErrNoRows) || err.Error() == "no rows in result set" {
				// Если ни одна строка не обновилась, значит версия в БД уже изменилась (конфликт!)
				http.Error(w, "Конфликт: ресурс уже изменен другим пользователем в БД", http.StatusConflict)
				return
			}
			http.Error(w, "Ошибка базы данных", http.StatusInternalServerError)
			return
		}

		// Возвращаем успешный результат с новой версией
		input.ID = id
		input.Version = newVersion
		json.NewEncoder(w).Encode(input)

	default:
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
	}
}
