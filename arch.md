# 📊 easymetric Finance — Архитектурная и техническая спецификация

Комплексный гайд по архитектуре, структуре модулей и исходному коду Telegram-бота `@easymetric_finance_bot` для персонального финансового учета на базе **Go + Fiber + Telebot**, Google Sheets API v4 и Google Gemini AI.

---

## 1. Концепция и ключевой Workflow

1. **Google Sheets как БД и Дашборд:** Пользователь копирует себе преднастроенный шаблон таблицы с диаграммами и формулами, выдает сервисному аккаунту бота права **Редактор** и отправляет ссылку/ID боту.
2. **Транспорт (Fiber + Webhook):** Telegram шлет HTTPS POST-запросы на эндпоинт `/webhook/telegram`. Fiber проверяет безопасность (Secret Token) и передает события в роутер Telebot.
3. **AI-интеграция (Gemini Function Calling):** Модель парсит свободный текст/голос, сопоставляет расход с динамическим списком категорий пользователя и формирует структурированную операцию.
4. **Управление категориями:** Пользователь может настроить категории как в таблице, так и голосом/текстом через интерактивное меню бота.

---

## 2. Структура проекта (Standard Go Layout)

```text
easymetric-finance/
├── cmd/
│   └── bot/
│       └── main.go                 # Точка входа: сборка конфигурации и DI
├── config/
│   └── config.go                   # Загрузка и валидация .env
├── internal/
│   ├── app/
│   │   └── app.go                  # Инициализация сервисов, Fiber, Graceful Shutdown
│   ├── domain/                     # Чистые доменные сущности (без внешних зависимостей)
│   │   ├── transaction.go          # Модели транзакций (Расход / Доход)
│   │   ├── category.go             # Модели категорий и действий над ними
│   │   └── user.go                 # Модель пользователя и связка с SheetID
│   ├── handler/                    # Уровень доставки (Delivery Layer)
│   │   ├── http/                   # HTTP-обработчики Fiber
│   │   │   └── webhook_handler.go  # Прием POST-вебхуков от Telegram и /healthz
│   │   └── telegram/               # Telegram UI / Обработчики событий
│   │       ├── router.go           # Регистрация обработчиков Telebot
│   │       ├── command_handler.go  # /start, /categories, /help
│   │       ├── message_handler.go  # Обработка текстовых и голосовых сообщений
│   │       └── callback_handler.go # Обработка нажатий инлайн-кнопок
│   ├── service/                    # Слой бизнес-логики (Application Layer)
│   │   ├── transaction_service.go  # Оркестрация: AI-парсинг -> запись в таблицу
│   │   ├── category_service.go     # Управление категориями, мутации и кэш (TTL)
│   │   └── ai_service.go           # Вызовы Gemini Function Calling и STT
│   └── repository/                 # Слой доступа к данным (Data Layer)
│       ├── sheets/
│       │   └── client.go           # Google Sheets API v4 клиент (Service Account)
│       └── storage/
│           └── user_storage.go     # Хранилище связей UserID <-> SheetID
├── pkg/
│   └── logger/
│       └── logger.go               # Структурированный логгер
├── credentials.json                # Ключ Google Service Account (в .gitignore)
├── .env.example
├── .gitignore
├── Dockerfile
├── go.mod
└── go.sum