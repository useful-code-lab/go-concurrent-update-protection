# concur-res (Concurrent Resource Service)

Демонстрационный высококонкурентный сервис на Go. Проект иллюстрирует три ключевые стратегии предотвращения потери обновлений (Lost Update) при одновременной модификации одного ресурса несколькими пользователями в инфраструктуре Kubernetes.

---

## 🛠️ Запуск сервера в режиме разработки (Локально)

Для быстрой проверки базовой логики проекта без внешних зависимостей (в памяти):

```bash
go run main.go
```

### Пример первоначального запуска проекта в памяти

**Удачное выполнение (Юзер 1 обновляет первым):**
```bash
aleksey@aleksey:~/concur-res$ curl http://localhost:8080/resource
{"id":"1","data":"Старт","version":1}

aleksey@aleksey:~/concur-res$ curl -X PUT -d '{"data":"Привет от Юзера 1","version":1}' http://localhost:8080/resource
{"id":"1","data":"Привет от Юзера 1","version":2}
```

**Симуляция конфликта (Юзер 2 пытается отправить устаревшую версию 1):**
```bash
aleksey@aleksey:~/concur-res$ curl -X PUT -d '{"data":"Привет от Юзера 2","version":1}' http://localhost:8080/resource
Конфликт: ресурс уже изменен другим пользователем
```

---

## ☸️ Тестирование стратегий конкурентности в Kubernetes (Minikube)

Перед выполнением тестов убедитесь, что развернута вся инфраструктура (`k8s-infra.yaml`) и проброшены порты приложения:
```bash
# В отдельном терминале для доступа через localhost:
kubectl port-forward service/concur-res-lb 8080:8080
kubectl port-forward service/concur-res-lb 30080:8080
```

### 1. Проверка оптимистичной блокировки (`/resource/optimistic`)
Конкуренция обрабатывается на уровне СУБД PostgreSQL атомарным запросом `UPDATE ... WHERE version = X`.

**Запуск теста:**
```bash
curl -i -X PUT -d '{"data":"Победа Юзера 1","version":1}' http://localhost:30080/resource/optimistic & \
curl -i -X PUT -d '{"data":"Победа Юзера 2","version":1}' http://localhost:30080/resource/optimistic
wait
```

**Результат (Один `200 OK`, второй `409 Conflict`):**
```text
HTTP/1.1 200 OK
Content-Type: application/json
Date: Sat, 23 May 2026 13:23:46 GMT
Content-Length: 58

{"id":"1","data":"Победа Юзера 2","version":2}

HTTP/1.1 409 Conflict
Content-Type: text/plain; charset=utf-8
X-Content-Type-Options: nosniff
Date: Sat, 23 May 2026 13:23:46 GMT
Content-Length: 86

Конфликт: ресурс изменен другим пользователем
```

### 2. Проверка пессимистичной блокировки (`/resource/pessimistic`)
Запросы выстраиваются в жесткую очередь на уровне СУБД с помощью `SELECT ... FOR UPDATE`. Ошибок нет, клиенты ждут освобождения строки.

**Запуск теста:**
```bash
curl -i -X PUT -d '{"data":"Запись от А","version":2}' http://localhost:30080/resource/pessimistic & \
curl -i -X PUT -d '{"data":"Запись от Б","version":2}' http://localhost:30080/resource/pessimistic
wait
```

**Результат (Оба `200 OK` с задержкой, данные перезаписываются по очереди):**
```text
HTTP/1.1 200 OK
Content-Type: application/json
Date: Sat, 23 May 2026 13:25:15 GMT
Content-Length: 53

{"id":"1","data":"Запись от Б","version":3}

HTTP/1.1 200 OK
Content-Type: application/json
Date: Sat, 23 May 2026 13:25:18 GMT
Content-Length: 53

{"id":"1","data":"Запись от А","version":4}
```

### 3. Проверка распределенной блокировки с помощью Redis (`/resource/redis`)
Блокировка в оперативной памяти через единый кластерный `redislock`. Снижает нагрузку на СУБД, отсекая конкурентов на дальних подступах.

**Запуск теста:**
```bash
curl -i -X PUT -d '{"data":"Юзер 1 (Redis)"}' http://localhost:8080/resource/redis & \
curl -i -X PUT -d '{"data":"Юзер 2 (Redis)"}' http://localhost:8080/resource/redis
wait
```

**Результат (Один `423 Locked` мгновенно, второй `200 OK` после обработки):**
```text
HTTP/1.1 423 Locked
Content-Type: text/plain; charset=utf-8
X-Content-Type-Options: nosniff
Date: Sat, 23 May 2026 19:50:53 GMT
Content-Length: 80

Ресурс заблокирован в Redis, попробуйте позже

HTTP/1.1 200 OK
Content-Type: application/json
Date: Sat, 23 May 2026 19:50:56 GMT
Content-Length: 51

{"id":"1","data":"Юзер 1 (Redis)","version":2}
```
