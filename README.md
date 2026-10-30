# PluginSound

Плеер с топ-чартами: один веб-интерфейс (`www/`), упакованный в Windows `.exe` (Electron) и Android `.apk` (Capacitor).

## Сборка

Самый короткий путь: GitHub → Actions → **build-pluginsound** → *Run workflow*.
Артефакты: `PluginSound-exe` (portable `.exe`) и `PluginSound-apk` (`PluginSound.apk`, debug-подпись).

Локально:

```
npm install
npm run check          # проверки без сети
npm start              # запуск в Electron
npm run build:win      # dist/PluginSound-1.0.0-x64.exe   (на Windows)
npx cap add android && npx cap sync android
cd android && ./gradlew assembleDebug    # android/app/build/outputs/apk/debug/app-debug.apk
```

Нужны: Node 22, для APK ещё JDK 21 и Android SDK. После первого `npm install` закоммитьте `package-lock.json`.

## Источники чартов

| Источник | Ключи | Статус |
|---|---|---|
| Deezer | нет | основной, даёт и чарт, и 30-сек превью |
| Apple Music | нет | чарт по стране; звук подбирается через поиск Deezer |
| Яндекс Музыка | OAuth-токен | неофициальный API, формат ответа не проверен на живом сервисе |
| Spotify | Client ID + Secret | Top 50 Global; у новых приложений доступ к редакторским плейлистам может быть закрыт |
| SoundCloud | client_id | неофициальный `api-v2`, не проверен на живом сервисе |

Если источник недоступен, показывается баннер с причиной и демо-чарт из вымышленных треков.
Звук везде — 30-секундные превью Deezer; полные треки эти сервисы через такие API не отдают.

Все адреса и поля ответов лежат в одном месте: `www/js/sources.js`.

## Графика

Всё нарисовано SVG: `www/img/` (иконка, знак, иллюстрации пустых экранов, волна в hero) и `www/js/art.js` (уникальные обложки для треков без картинки).
PNG/ICO/Android-иконки собираются из SVG командой `npm run icons` (нужны Python, playwright, pillow).
