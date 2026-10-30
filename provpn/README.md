# ProVpn

Локальное меню для WireGuard. Приложение само ничего не туннелирует: хранит ваши профили и управляет WireGuard. Трафик идёт через сервер, чей конфиг вы добавили.

## Windows (exe)
1. Установите [WireGuard для Windows](https://www.wireguard.com/install/) и [Node.js 20+](https://nodejs.org) (Node нужен только для сборки).
2. Дважды щёлкните `build.cmd`, получится `dist\ProVpn.exe`.
3. Запустите `ProVpn.exe`: это самостоятельное окно приложения (Electron), браузер не нужен. Права администратора запрашиваются самим exe. Файл portable: установка не требуется.

Профили хранятся в `%APPDATA%\ProVpn\profiles`. Подключение: `wireguard.exe /installtunnelservice`, отключение: `/uninstalltunnelservice`.

## Linux / macOS
```
sudo node server.js        # http://localhost:7878
```
Нужны `wg` и `wg-quick` в PATH.

## Тесты
`npm test`: проверка логики со stub-версией wg (POSIX и Windows-ветка). Реальный туннель не создаётся; на Windows-хосте тест не запускается (stub на sh).

## Безопасность
- Слушает только 127.0.0.1; проверяются Host и Origin (DNS rebinding, CSRF)
- Конфиги с `PreUp/PostUp/PreDown/PostDown` отклоняются: они выполняют shell-команды
- Чужие бесплатные серверы видят весь ваш трафик
