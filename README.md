# Video downloader

Один Go-процесс: HTTP API, Telegram webhook и конкурентная обработка видео в goroutine. RabbitMQ, S3/B2, файлового хранилища, очередей и отдельных воркеров нет.

```text
POST /api/downloads → goroutine → yt-dlp + FFmpeg → буфер в RAM → GET .../file
POST /webhook       → goroutine → yt-dlp + FFmpeg → буфер в RAM → Telegram
```

yt-dlp получает ссылки на дорожки, FFmpeg передаёт MP4 через stdout. Видео, фрагменты и результаты обработки не записываются на диск. Звук и видео объединяются в потоковый MP4 без перекодирования. Выбираются H.264 + M4A либо готовый MP4; поддерживаются прямые HTTP(S) и HLS-потоки. Если подходящего формата нет, задача завершается ошибкой. Это не универсальный конвертер кодеков.

## Структура

```text
internal/
  handler/
    api/          HTTP-запросы и ответы веб-API
    telegram/     приём Telegram webhook
  service/
    web/          веб-сервис: создание, статус и выдача задач
    telegram/     Telegram-сервис: обработка updates и отправка видео
  infrastructure/
    ytdlp/        вызов yt-dlp и потокового FFmpeg
    command/      запуск и отмена внешних процессов
    telegram/     HTTP-клиент Telegram Bot API
  model/          общие типы видео, задач и проверка ссылок
  app/            сборка зависимостей, запуск HTTP/CLI и общий лимит goroutine
  config/         настройки окружения
  errors/         общие ошибки
```

Каждый handler вызывает свой сервис: web или telegram. Каждый сервис находится в своей папке и зависит от интерфейсов; реализации внешних операций находятся в infrastructure. Общий запуск goroutine и завершение задач находятся в `app/concurrency.go` и передаются обоим сервисам через интерфейс `Tasks`. Отдельного пакета или сервиса конкурентности нет. Repository не нужен: постоянного хранения нет, состояние активных задач и временные буферы находятся в памяти процесса.

## Запуск

```sh
make init
# Заполните TELEGRAM_BOT_TOKEN, TELEGRAM_WEBHOOK_SECRET, WEB_API_TOKEN в .env.
make up
make health
make logs
```

Compose запускает только приложение; внешняя Docker-сеть и volume для видео не нужны. Порт по умолчанию — 8085. После изменения env или кода выполните `make up`.

Локально нужны Go 1.25.5, yt-dlp с EJS и curl-cffi, FFmpeg и Node.js. curl-cffi нужен загрузчику для браузерных HTTP/TLS-запросов к TikTok; в Docker он устанавливается через `yt-dlp[default,curl-cffi]` ([документация yt-dlp](https://github.com/yt-dlp/yt-dlp#impersonation)).

```sh
python3 -m pip install -U "yt-dlp[default,curl-cffi]"
```

После изменения Dockerfile пересоберите образ. На Render выполните **Manual Deploy → Deploy latest commit** после отправки изменений в подключённый репозиторий.

Локальный запуск:

```sh
make run
# CLI выводит бинарный MP4 в stdout, аргумента output-dir больше нет:
go run ./cmd/app 'https://www.youtube.com/watch?v=VIDEO_ID' | player -
```

Для Telegram:

```sh
make webhook-set WEBHOOK_URL=https://your-host/webhook
make webhook-info
```

Frontend находится в соседнем проекте: `make front-up`, `make front-logs`.

## API

Все `/api/` требуют `Authorization: Bearer <WEB_API_TOKEN>`.

- `POST /api/downloads` с JSON `{"url":"https://..."}` возвращает `202` и задачу со статусом `downloading`.
- `GET /api/downloads/{id}` возвращает статус `downloading`, `ready` или `failed`.
- `GET /api/downloads/{id}/file` отдаёт MP4 из памяти; Range-запросы поддерживаются.
- `GET /healthz` проверяет доступность HTTP.

Лимит одновременных задач общий для API и Telegram. Если свободных слотов нет, запрос получает `503`; ожидающей очереди нет. После принятия задача продолжает выполняться независимо от соединения, которым она создана.

## Лимиты и время жизни

| Переменная | По умолчанию | Назначение |
| --- | --- | --- |
| `MAX_CONCURRENT_DOWNLOADS` | `2` | Одновременные задачи, включая отправку в Telegram |
| `MAX_UPLOAD_BYTES` | `50000000` | Максимальный размер одного видео для API и Telegram |
| `MAX_MEMORY_BYTES` | `200000000` | Общий бюджет буферов видео |
| `MAX_WEB_JOBS` | `100` | Число веб-задач, включая ошибки |
| `DOWNLOAD_TIMEOUT` | `20m` | Таймаут загрузки |
| `SEND_TIMEOUT` | `5m` | Таймаут отправки в Telegram |
| `WEB_FILE_TTL` | `10m` | Время доступности результата после завершения |

На каждое загружаемое видео резервируется `MAX_UPLOAD_BYTES`. Резерв остаётся за веб-результатом до TTL и закрытия всех открытых читателей. Поэтому число одновременно хранимых видео ограничено отношением `MAX_MEMORY_BYTES / MAX_UPLOAD_BYTES`. При нехватке памяти принятая задача завершается ошибкой. Telegram освобождает буфер после отправки. Таймер каждой веб-задачи удаляет её и освобождает буфер; периодического воркера нет. После удаления API возвращает `404`.

Бюджет относится к видео; Go, yt-dlp, FFmpeg и HTTP требуют дополнительной памяти. Значения выбирайте с учётом RAM контейнера.

Задачи, видео и защита от повторных Telegram updates находятся только в RAM. Перезапуск всё очищает; восстановления и автоматических повторов после ошибок нет. Telegram webhook подтверждается при запуске задачи; последующая ошибка попадает в лог. Повторные updates подавляются во время обработки и час после успешной отправки. При завершении приложения задачи отменяются, subprocess завершаются, приложение ждёт выхода goroutine. Используйте один экземпляр приложения или маршрутизацию запросов к тому же экземпляру.

## Cookies

Без `YOUTUBE_COOKIES_BROWSER` и `YOUTUBE_COOKIES_FILE` загрузка анонимная. При необходимости сохраните Netscape cookies в `secrets/youtube.txt` и задайте `YOUTUBE_COOKIES_FILE=/run/downloader-secrets/youtube.txt`. Файл должен читаться пользователем контейнера UID 10001. Для yt-dlp создаётся отдельная временная копия cookies, удаляемая после задачи; исходный файл остаётся неизменным. Видео во временные файлы не записываются.

## Проверки

```sh
make check
go test -race ./...
RUN_MEDIA_INTEGRATION=1 go test ./internal/infrastructure/ytdlp -run TestStreamIntegration
```

Тесты проверяют конкурентные лимиты, отмену, ошибки/TTL задач, время жизни буферов и HTTP-выдачу из памяти. Реальные загрузки зависят от доступности источника и cookies.
