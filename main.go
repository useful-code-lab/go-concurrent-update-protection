package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
)

// Resource — структура нашего документа.
// Поле Version защищает от одновременной перезаписи.
type Resource struct {
	ID      string `json:"id"`
	Data    string `json:"data"`
	Version int    `json:"version"`
}

// Хранилище в памяти и мьютекс для защиты от Race Condition
var (
	mutex    sync.Mutex
	resource = Resource{ID: "1", Data: "Старт", Version: 1}
)

func main() {
	// Регистрируем один простой маршрут
	http.HandleFunc("/resource", handleResource)

	fmt.Println("Сервер проекта concur-res запущен на порту :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handleResource(w http.ResponseWriter, r *http.Request) {
	// Устанавливаем заголовок JSON для всех ответов
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	
	// 1. Чтение ресурса (GET)
	case http.MethodGet:
		mutex.Lock()
		json.NewEncoder(w).Encode(resource)
		mutex.Unlock()

	// 2. Обновление ресурса (PUT)
	case http.MethodPut:
		var input Resource
		// Читаем то, что прислал пользователь
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "Ошибка в JSON", http.StatusBadRequest)
			return
		}

		mutex.Lock()
		// ГЛАВНАЯ ПРОВЕРКА: совпадает ли версия пользователя с версией в памяти?
		if input.Version != resource.Version {
			mutex.Unlock()
			// Если версии разные, отдаем статус 409 (Conflict)
			http.Error(w, "Конфликт: ресурс уже изменен другим пользователем", http.StatusConflict)
			return
		}

		// Если версии совпали: обновляем данные и увеличиваем версию на +1
		resource.Data = input.Data
		resource.Version++
		
		// Отдаем клиенту обновленный объект
		json.NewEncoder(w).Encode(resource)
		mutex.Unlock()

	default:
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
	}
}
