# Video downloader

Один backend-процесс и контейнер `video-downloader`: HTTP webhook, HTTP API и три consumer’а RabbitMQ запускаются вместе. PostgreSQL не используется.

```text
POST /webhook       → video.download → Telegram download handler → telegram.send → Telegram
POST /api/downloads → web.download   → Web download handler       → файл для HTTP-скачивания
```

Имена очередей дополняются `QUEUE_PREFIX` (в текущем окружении `myversion.`). HTTP принимает запрос, проверяет его и подтверждает постановку в RabbitMQ. Скачивание выполняется consumer’ами. Чтение статуса, готового файла и health check выполняются непосредственно через HTTP.

## Запуск

```sh
make init       # создаёт .env из примера только при отсутствии
# Заполнить .env
make up         # сборка и запуск одного backend-контейнера
make logs
make health
```

`docker-compose.yml` подключается к существующей сети `tool_default`; адрес RabbitMQ задаётся только через `RABBIT_AMQP_URL`. Новые RabbitMQ/PostgreSQL не создаются. Проект Compose и volume downloads сохранены; `make up` удаляет старые контейнеры отдельных worker’ов этого проекта, сохраняя данные.

HTTP слушает `:8085` внутри контейнера, порт хоста задаётся `HTTP_PORT`. CloudPub должен проксировать `http://localhost:8085`.

```sh
make webhook-set WEBHOOK_URL=https://searchingly-encouraged-topi.cloudpub.ru/webhook
make webhook-info
make webhook-delete
make ps
make restart
make down
```

Регистрация webhook выполняется явно. Скрипт использует секреты из `.env`, проверяет ответ Telegram и не сбрасывает ожидающие updates. Для скрипта нужны curl и python3. `make restart` перезапускает текущий контейнер; после изменения env или кода используйте `make up`.

Frontend остаётся в соседнем проекте `../DownloaderPRMYVERSIONFront`, доступен на порту 3001:
```sh
make front-up
make front-logs
make front-down
```

## Локальный запуск

Нужны Go 1.25.5, Node.js 22, FFmpeg/ffprobe, yt-dlp с EJS и gallery-dl.
В локальном env укажите `RABBIT_AMQP_URL` с адресом, доступным с вашей машины, `DOWNLOAD_DIR=./downloads`, `HTTP_ADDR=:8085`. Для контейнера адрес в URL должен быть доступен из Docker-сети.
`make run` экспортирует значения из `.env` и запускает все компоненты; другой env можно выбрать через `ENV_FILE`.
```sh
make deps
make run
# Прямое скачивание без очереди:
go run -buildvcs=false ./cmd/app 'https://www.youtube.com/watch?v=VIDEO_ID' ./downloads
```

Запуск бинарника без аргументов эквивалентен `downloader serve`. CLI с URL печатает JSON с `hash` и `name`.

## Структура

```text
cmd/app/main.go                   точка входа, сигналы ОС
internal/app                     app.go, routes.go, http.go, download.go, cli.go
internal/config                  environment variables
internal/errors                  классификация ошибок
internal/worker                  периодическая очистка файлов
internal/handler/
  telegram/                      Telegram webhook
  api/                           HTTP API
  queue/                         декодирование сообщений RabbitMQ
internal/service/
  model/                         структуры задач, файлов и названия очередей
  video/                         определение платформы и извлечение URL
  telegram/                      постановка, скачивание и отправка Telegram
  web/                           web-задачи и доступ к результату
  download/                      выбор загрузчика, подготовка MP4
internal/infrastructure/
  rabbitmq/ telegram/            внешние API
  gostreampuller/ gallerydl/      адаптеры загрузчиков
  ffmpeg/ command/               конвертация и внешние процессы
internal/storage/
  files/                         файловое состояние задач и отметки отправки
  media/                         временные директории, проверка и SHA-256
scripts/webhook.sh               управление webhook
```

Интерфейсы объявлены у потребителей; общего пакета interfaces нет. Зависимости внедряются конструкторами. Оба загрузчика реализуют `Download(context.Context, URL, outputDir) (model.File, error)`. Имя результата — `<SHA-256>.<extension>`. Отдельного domain-слоя нет.

YouTube/Instagram используют существующий yt-dlp-путь внутри адаптера gostreampuller, TikTok — gallery-dl. `VIDEO_BACKEND=gostreampuller` включает API установленной библиотеки v1.1.0 в отменяемом дочернем процессе. В этом режиме применяются `VIDEO_FORMAT`, `VIDEO_RESOLUTION`, `VIDEO_CODEC`; параметры cookies и `YT_DLP_BIN` относятся к CLI-режиму.

## HTTP и файлы

- `POST /webhook`: заголовок `X-Telegram-Bot-Api-Secret-Token`.
- `POST /api/downloads`: JSON `{"url":"https://..."}`, ответ 202 с задачей.
- `GET /api/downloads/{id}`: состояние задачи.
- `GET /api/downloads/{id}/file`: готовый файл.
- `GET /healthz`: доступность HTTP.

API требует `Authorization: Bearer <WEB_API_TOKEN>`. Frontend хранит этот токен на своей серверной стороне.

Для Telegram результат преобразуется в MP4/H.264/AAC и отправляется через sendVideo. При превышении `MAX_UPLOAD_BYTES=50000000` применяется двухпроходное сжатие: качество может снизиться. После успешной отправки файл удаляется.

Web сохраняет исходное качество и контейнер без Telegram-лимита. Файл доступен до `WEB_FILE_TTL` (по умолчанию 24 часа), затем удаляется. Завершившаяся ошибка скачивания отображается в статусе; повтор из UI создаёт новую задачу.

Очистка Web-файлов запускается при старте приложения и каждые `CLEANUP_INTERVAL` (по умолчанию 1 минута). Активные задачи и открытые HTTP-скачивания защищены до завершения обработки/закрытия файла. Очистка завершится вместе с приложением; после простоя просроченные файлы удалятся при следующем старте. Отдельный cron не требуется.

В текущем Docker-окружении файлы расположены так:

- Telegram: `/data/downloads/<job-id>/<hash>.mp4`.
- Web: `/data/downloads/web/<job-id>/<hash>.<extension>`.
- Volume: `downloader-myversion_downloads`.
- Путь volume на этом хосте: `/var/lib/docker/volumes/downloader-myversion_downloads/_data`.

При локальном запуске корень задаёт `DOWNLOAD_DIR` (по умолчанию `./downloads`). Compose использует `/data/downloads`. Просроченные Web-файлы удаляются без корзины; при необходимости их нужно скачать повторно. Telegram-файлы неудачных/ожидающих задач автоматически по возрасту не удаляются: на них могут ссылаться retry/DLQ. Для их очистки сначала нужно решить судьбу соответствующих задач. Отметки `.sent` также сохраняются для защиты от повторной отправки.

## Очереди и остановка

Durable queues, persistent messages, publisher confirms, manual ack и prefetch=1. Временные ошибки повторяются до `MAX_RETRIES` через очередь .retry с задержкой 10 секунд; постоянные ошибки и исчерпанные попытки попадают в .dead. Неоднозначная ошибка публикации сохраняет исходную задачу неподтверждённой.

SIGTERM отменяет текущую работу. Прерванные задачи возвращаются RabbitMQ после закрытия канала; HTTP получает до 20 секунд на завершение. Сбой любого consumer’а останавливает всё приложение, которое перезапускает Compose.

Файловый cache и отметки отправки уменьшают повторную работу, но доставка остаётся at-least-once: сбой после принятия файла Telegram может привести к повторной отправке. Запускайте один экземпляр приложения: распределённые блокировки не реализованы. Файлы неудачных Telegram-задач и отметки .sent сохраняются; контролируйте свободное место.

## YouTube cookies

При пустых `YOUTUBE_COOKIES_BROWSER` и `YOUTUBE_COOKIES_FILE` загрузчик работает без cookies. Публичные видео часто доступны; гарантировать все ссылки без авторизации нельзя.

Если YouTube требует вход, экспортируйте cookies YouTube в Netscape-формате из отдельной приватной сессии: после входа откройте в той же вкладке https://www.youtube.com/robots.txt, экспортируйте cookies и закройте сессию. Сохраните файл в `secrets/youtube.txt`, обеспечив чтение пользователю контейнера UID 10001, и задайте:
```dotenv
YOUTUBE_COOKIES_FILE=/run/downloader-secrets/youtube.txt
```
Примените через `make up`. Cookies и .env не должны попадать в Git.

## Проверки

```sh
make fmt
make check
```

По запросу все файлы `*_test.go` удалены. `make check` проверяет форматирование, запускает go vet, go test (компиляция пакетов без тестов) и сборку.
# DownoladerBack
