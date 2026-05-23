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