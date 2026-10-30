# ChessVision для Android

Приложение читает шахматную доску с экрана телефона (запись экрана MediaProjection),
сама определяет доску, фигуры и вашу сторону и рисует стрелку лучшего хода поверх
шахматного приложения. Ядро (распознавание, трекер ходов, движок) — тот же Go-код, что и в
Windows-версии, подключённый через `gomobile bind` (`mobile/`).

## Как получить APK (без установки Android Studio)

1. Создайте на GitHub пустой репозиторий и загрузите в него всё содержимое этой папки
   (вместе с `.github/`).
2. Откройте вкладку **Actions → Build Android APK → Run workflow** (или просто сделайте push).
3. Через ~10–15 минут в запуске появится артефакт **ChessVision-apk** — скачайте `ChessVision.apk`.
4. Перенесите APK на телефон и установите (разрешите установку из этого источника).

## Локальная сборка

Нужны Go 1.24, JDK 17, Android SDK 34 и NDK `26.1.10909125`.

```
go install golang.org/x/mobile/cmd/gomobile@latest
go install golang.org/x/mobile/cmd/gobind@latest
go get golang.org/x/mobile/bind@latest
gomobile init
gomobile bind -target=android/arm64,android/arm -androidapi 24 -o android/app/libs/cvmobile.aar ./mobile
cd android && gradle assembleDebug
```

APK: `android/app/build/outputs/apk/debug/app-debug.apk`.

## Использование

Запустите ChessVision → выдайте «Поверх других приложений» и запись экрана → откройте
шахматное приложение → в плавающей панели нажмите «Старт». Панель можно перетаскивать за
заголовок и сворачивать. Если доска сдвинулась или вы повернули телефон — «Найти доску».

Используйте для анализа партий и тренировки. В онлайн-играх с живыми соперниками помощь
движка запрещена правилами платформ.
