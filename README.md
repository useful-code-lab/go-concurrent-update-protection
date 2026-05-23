## Запуск сервера на порту
```bash
go run main.go
```

## Пример первоначального запуска проекту

### Удачное выполнение
```bash
aleksey@aleksey:~/concur-res$ curl http://localhost:8080/resource
# Ответ: {"id":"1","data":"Старт","version":1}
{"id":"1","data":"Старт","version":1}
aleksey@aleksey:~/concur-res$ curl -X PUT -d '{"data":"Привет от Юзера 1","version":1}' http://localhost:8080/resource
# Ответ: {"id":"1","data":"Привет от Юзера 1","version":2}
{"id":"1","data":"Привет от Юзера 1","version":2}
aleksey@aleksey:~/concur-res$ 
```

### Симуляция конфликта
```bash
curl -X PUT -d '{"data":"Привет от Юзера 2","version":1}' http://localhost:8080/resource
# Ответ: Конфликт: ресурс уже изменен другим пользователем
``` 

## Проверка оптимистичной блокировки
```bash
curl -i -X PUT -d '{"data":"Победа Юзера 1","version":1}' http://localhost:30080/resource/optimistic & curl -i -X PUT -d '{"data":"Победа Юзера 2","version":1}' http://localhost:30080/resource/optimistic
[1] 122646
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

## Проверка пессимистичной блокировки
```bash
 curl -i -X PUT -d '{"data":"Запись от А","version":2}' http://localhost:30080/resource/pessimistic & \
curl -i -X PUT -d '{"data":"Запись от Б","version":2}' http://localhost:30080/resource/pessimistic
wait
[1]+  Завершён        curl -i -X PUT -d '{"data":"Победа Юзера 1","version":1}' http://localhost:30080/resource/optimistic
[1] 124652
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
[1]+  Завершён        curl -i -X PUT -d '{"data":"Запись от А","version":2}' http://localhost:30080/resource/pessimistic
```

## Проверка блокировки с помощью Redis
```bash
$ curl -i -X PUT -d '{"data":"Юзер 1 (Redis)"}' http://localhost:8080/resource/redis & \
curl -i -X PUT -d '{"data":"Юзер 2 (Redis)"}' http://localhost:8080/resource/redis
wait
[1] 39428
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
[1]+  Завершён        curl -i -X PUT -d '{"data":"Юзер 1 (Redis)"}' http://localhost:8080/resource/redis
```