# ProVpn

Десктопный клиент: WireGuard, VLESS и Trojan. Приложение хранит ваши профили и поднимает туннель; трафик идёт через сервер, чей конфиг вы добавили.

## Windows (exe)
1. Установите [Node.js 20+](https://nodejs.org) (нужен только для сборки) и, если будете использовать WireGuard, [WireGuard для Windows](https://www.wireguard.com/install/). Для VLESS/Trojan WireGuard не нужен.
2. Дважды щёлкните `build.cmd`: он скачает ядро sing-box 1.11.x и соберёт `dist\ProVpn.exe`.
3. Запустите `ProVpn.exe`: это самостоятельное окно приложения (Electron). Права администратора запрашиваются самим exe. Файл portable: установка не требуется.

Профили хранятся в `%APPDATA%\ProVpn\profiles`.

## Протоколы
| Протокол | Как работает |
|---|---|
| WireGuard | `wireguard.exe /installtunnelservice` (служба Windows, переживает закрытие окна) |
| VLESS (tcp, ws, grpc, http, httpupgrade; tls, Reality, flow xtls-rprx-vision) | ядро sing-box в режиме TUN: весь трафик системы идёт через сервер |
| Trojan (tcp, ws, grpc, http, httpupgrade; tls) | то же |

VLESS/Trojan-туннель живёт, пока открыто окно ProVpn: при закрытии ядро останавливается. В этом режиме нет счётчиков трафика, вместо рукопожатия показывается время работы. Не поддерживаются: vmess, ss, hysteria, tuic, транспорты xhttp/kcp/quic.

Добавить сервер вручную: «Добавить» → вставьте ссылку `vless://…` или `trojan://…` (или WireGuard-конфиг).

## Подписка по URL
Кнопка «Подписка» → вставьте ссылку, ProVpn скачает её и создаст профили. Понимает (в том числе в base64):
- ссылки `vless://`, `trojan://`, `wireguard://PRIVATEKEY@host:port?publickey=...&address=...#имя`
- обычные WireGuard `.conf` (несколько `[Interface]` подряд)
- sing-box JSON (outbounds `wireguard`, `vless`, `trojan`)

Записи с другими протоколами пропускаются и показываются как «не поддерживается». Кнопки рядом с подпиской: обновить (заменяет её серверы) и удалить. Ссылка хранится в `_subscriptions.json` (права 0600) и в интерфейс не отдаётся.

## Linux / macOS (режим разработки)
```
sudo node server.js        # http://localhost:7878
```
WireGuard: `wg` и `wg-quick` в PATH. VLESS/Trojan: `sing-box` в PATH (или `PROVPN_SINGBOX`).

## Тесты
- `npm test`: логика со stub-версиями wg и sing-box (POSIX и Windows-ветка). Реальный туннель не создаётся.
- `node test/e2e-core.js <sing-box>`: настоящий sing-box (`check` конфигов + трафик VLESS/Trojan через loopback); запускается в CI.

## Диагностика
Журнал: `%APPDATA%\ProVpn\provpn.log` (подключения, ошибки, обрывы ядра; без паролей и UUID). Если окно не открывается из-за видеодрайвера, создайте пустой файл `%APPDATA%\ProVpn\disable-gpu`.

Подключение и отключение выполняются фоновой задачей: окно запрашивает состояние и показывает результат, поэтому смена сети при подъёме туннеля не рвёт операцию. Если ядро VLESS/Trojan остановилось само, окно сообщает об обрыве.

## Безопасность
- Слушает только 127.0.0.1; проверяются Host и Origin (DNS rebinding, CSRF)
- WireGuard-конфиги с `PreUp/PostUp/PreDown/PostDown` отклоняются: они выполняют shell-команды
- VLESS/Trojan: из ссылок и JSON берутся только известные поля, значения с переводами строк отклоняются; конфиг ядра хранится с правами 0600 и удаляется при остановке
- Чужие бесплатные серверы видят весь ваш трафик
