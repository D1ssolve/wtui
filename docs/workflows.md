# От задачи до релиза

[English](workflows.en.md)

Короткий путь для пресета `git-flow` (`workflow: {extends: git-flow}`): `develop` — интеграция, `master` — production. Раздел [Trunk-based](#trunk-based) описывает `extends: trunk-based`. Команда `wtui flow` показывает, что ваша конфигурация делает на каждом шаге.

## Перед началом

1. Создайте конфиг командой `wtui init` (см. [README](../README.md)) и проверьте его через `wtui flow`. Без `--force` init не перезаписывает существующий конфиг.
2. Авторизуйте `glab` для GitLab или `gh` для GitHub.
3. В wtui нажмите `,`, чтобы увидеть effective config, и `.`, чтобы проверить инструменты. `W` показывает схему workflow; `wtui flow --html > flow.html` сохраняет её как страницу, которая открывается офлайн.

**Task** объединяет worktrees и одноимённые ветки нескольких сервисов. **MR** создаёт wtui; review и CI проходят в GitHub/GitLab, а готовые MR wtui проверяет, подтверждает и сливает через `M`. **Release** готовит версии, production MR и теги.

## Feature

```mermaid
flowchart LR
    A["i: создать задачу"] --> B["код и commits"]
    B --> C["V: проверить Git-состояние"]
    C --> D["C: создать MR"]
    D --> E["review и CI в forge"]
    E --> F["M: проверить, подтвердить и слить MR"]
    F --> G["D/P: очистить вручную"]
```

1. В Tasks (`1`) нажмите `i`, укажите ID и сервисы. Для каждого сервиса wtui создаст `feature/<TASK>` и worktree в `<tasks_root>/<TASK>/<service>`.
![Диалог создания feature-задачи в wtui](images/workflows/feature-01-init.png)
2. Нажмите `O` для редактора или `R` для Rider. Пишите код, запускайте тесты и делайте commits как обычно. В Services (`Enter` или `2`) можно добавить сервис клавишей `a` или открыть lazygit клавишей `Enter` на выбранном сервисе.
![Состояние feature-задачи в wtui перед работой во внешних инструментах](images/workflows/feature-02-overview.png)
3. Нажмите `V`. Это проверка Git-состояния, а не тесты приложения. `S` синхронизирует ветки с remote.
![Ошибка проверки Git-состояния feature-задачи в wtui](images/workflows/feature-03-validation-error.png)
4. Нажмите `C`, проверьте план и подтвердите. Если тип закрывается шагом `review` (как `feature` в пресете), wtui пушит ветки и создаёт MR в `develop`. Шаг `merge` обновляет target-ветки напрямую, без MR.
![Подтверждение Close для feature-задачи в wtui](images/workflows/feature-04-close-confirm.png)
5. Проведите review и дождитесь CI в forge. wtui не удаляет задачу.
![Готовые MR перед merge через wtui](images/workflows/feature-05-mr-ready.png)
6. Когда MR готовы, нажмите `M`: wtui повторно проверит readiness, запросит подтверждение, сольёт готовые MR и повторно проверит и сохранит результат. Для одного сервиса: Services → сервис → `m`.
![Сверка состояния MR в wtui](images/workflows/feature-06-mr-reconciliation.png)
7. Когда задача больше не нужна, запустите ручную очистку через `D` или `P`.
![Кандидаты на ручную очистку задач в wtui](images/workflows/cleanup-candidates-80x24.png)

## Release

```mermaid
flowchart LR
    A["3, N: создать Release"] --> B["prepare"]
    B --> C["regression"]
    C --> D["F: production MR"]
    D --> E["review и CI в forge, M: merge"]
    E --> F["F: теги"]
    F --> G["D/P: очистить вручную"]
```

1. В Releases (`3`) нажмите `N`, выберите root-задачи типа по умолчанию (`feature`) и версии сервисов. Предложенная версия — следующий patch от локального semver-тега; без тегов — `0.1.0`.
![Выбор feature-задачи для нового release в wtui](images/workflows/release-01-create.png)
2. Проверьте preview и подтвердите prepare. По умолчанию каждая task-ветка уже должна быть в `origin/develop`. С шагом релиза `integrate_tasks` вместо этого нужен один ready MR каждого сервиса в `develop`.
![Подтверждение prepare release в wtui](images/workflows/release-02-prepare-confirm.png)
3. В статусе `prepared` выполните regression вручную. Если нужны исправления, протестируйте, закоммитьте и опубликуйте их в release-worktree до promote.
![Статус prepared в wtui перед внешней regression](images/workflows/release-03-prepared.png)
4. Нажмите `F`. wtui создаёт MR `release/<version> → master` для каждого сервиса.
![Статус prepared в wtui перед созданием production MR](images/workflows/release-03-prepared.png)
5. Проведите review и дождитесь CI в forge, затем нажмите `M`. wtui проверит, подтвердит, сольёт готовые MR и сохранит принятый merge SHA.
![Статус ожидающего merge через wtui](images/workflows/release-workflow-120x40.png)
6. Нажмите `F` ещё раз. wtui проверит `master`, вернёт release-ветку в `develop`, создаст и запушит annotated теги. Статус `released` не означает deploy.
![Статус released в wtui после тегирования](images/workflows/release-06-released.png)
7. При необходимости очистите released release через `D` или `P`.
![План ручной очистки released release в wtui](images/workflows/release-cleanup-80x24.png)

Временные integration-worktrees — внутренний ресурс prepare. По умолчанию wtui удаляет их; `release.keep_integration_worktrees: true` сохраняет их для отладки. Это не удаляет task или release каталог.

## Hotfix

```mermaid
flowchart LR
    A["i: hotfix"] --> B["код, V"]
    B --> C["C: MR в master и develop"]
    C --> D["review и CI в forge, M: merge"]
    D --> E["C: теги"]
    E --> F["D/P: очистить вручную"]
```

Шаги ниже — для hotfix, который закрывается через MR:

```yaml
workflow:
  extends: git-flow
  task_types:
    hotfix:
      prefixes: ["hotfix/"]
      from: production
      shared: true
      close:
        - review: {into: [production, integration]}
        - tag: {on: production}
```

Hotfix самого пресета вместо этого сливается в `master` и `develop` напрямую и ставит тег одним `C`.

1. Создайте задачу через `i`, выберите тип `hotfix`. Ветка `hotfix/<TASK>` начинается от `master`.
![Диалог создания hotfix от master в wtui](images/workflows/hotfix-01-init.png)
2. Разрабатывайте и проверяйте Git-состояние так же, как feature-задачу.
![Состояние hotfix-задачи в wtui после проверки Git-состояния](images/workflows/hotfix-02-overview.png)
3. Нажмите `C`: wtui создаёт MR и в `master`, и в `develop`.
![Подтверждение post-actions hotfix с MR в wtui](images/workflows/hotfix-03-close-mr.png)
4. Проведите review и дождитесь CI для обоих MR в forge, затем нажмите `M`: wtui проверит, подтвердит и сольёт готовые MR, после чего повторно проверит и сохранит результат.
![Статус существующих MR hotfix в master и develop перед merge через wtui](images/workflows/hotfix-04-mr-status.png)
5. Повторите `C` для версий и тегов. При частичном сбое повторите `C`: checkpoint сохраняет подтверждённые версии.
![Подтверждение версии и тега hotfix в wtui](images/workflows/hotfix-05-tag-confirm.png)
6. Очистите задачу только после завершения обоих merge и post-actions.
![Кандидаты на ручную очистку hotfix в wtui](images/workflows/cleanup-candidates-80x24.png)

`H` на hotfix конвертирует задачу в feature: создаёт `feature/<TARGET>` от подтверждённого SHA, пушит ветки с lease и удаляет только неизменённые локальные hotfix-ветки. Remote source-ветка сохраняется.

## Trunk-based

С `workflow: {extends: trunk-based}` есть одна долгоживущая ветка — `main`.

```mermaid
flowchart LR
    A["i: change"] --> B["C: squash MR в main"]
    B --> C["M: merge"]
    C --> D["3, N: отрезать release/<version> от main"]
    D --> E["qualification"]
    E --> F["F: тег на release-ветке"]
```

1. Задача без известного префикса — это `change`: ветка называется ID задачи, начинается от `main`, а `C` открывает squash MR в `main`.
2. Релиз отрезается от `main` (`N`). Production MR и back-merge нет.
3. Баг, найденный на qualification, чинится в `main` задачей `fix/<TASK>`. После merge её MR `C` переносит принятый merge в release-ветку каждого открытого релиза (`git cherry-pick -x`, push с lease). Релизы, где фикс уже есть, пропускаются; релизы с уже принятым коммитом тега помечаются как «не перенесено».
4. `F` ставит тег на текущую вершину release-ветки. Повтор после сбоя сохраняет этот коммит и отказывается, если ветку переписали.

## Свой жизненный цикл релиза

Релиз работает на state machine. Кроме пресетов через `extends` её можно описать в `workflow.release.machine`: QA с циклом «отклонить → исправить», хуки деплоя на staging, ожидание зелёного CI, цепочки окружений вроде `main → pre-production → production`. В панели Releases футер показывает клавиши текущего состояния; `F` продвигает релиз или перепроверяет условие, `M` сливает ревью, которых ждёт релиз. Подробно: [Release state machine](configuration.md#release-state-machine).

## Ручная очистка

Ничего не удаляется автоматически после Close, merge, тегов или finalize.

1. Нажмите `D` или `P` в Tasks либо Releases.
![Список кандидатов ручной очистки в wtui](images/workflows/cleanup-candidates-80x24.png)
2. wtui покажет read-only список кандидатов.
![Read-only кандидаты очистки в wtui](images/workflows/cleanup-candidates-80x24.png)
3. Выберите задачи или released releases.
![Выбранный кандидат ручной очистки в wtui](images/workflows/cleanup-candidates-selected-80x24.png)
4. Каждый выбранный элемент заново планируется и требует отдельного подтверждения.
![Подтверждение очистки задачи в wtui](images/workflows/task-cleanup-confirm-120x40.png)

Очистка удаляет worktrees, сгенерированные метаданные и task/release каталоги. Локальные и remote ветки, а также теги, всегда сохраняются. Каталог с неизвестными файлами сохраняется и возвращает ошибку вместо рекурсивного удаления.

Подробнее: [cleanup.md](cleanup.md). Все ключи и ограничения: [configuration.md](configuration.md).

## Если операция остановилась

| Где | Что делать |
|---|---|
| Close | Проверьте push и MR, затем повторите `C`. |
| Merge check | После устранения blocker повторите `M`; он заново проверит MR перед merge. |
| Release | При recoverable `failed` используйте `Ctrl+R` в Releases. После частичного production merge используйте `M`, не `Ctrl+R`. |
| Cleanup | Уже удалённое не откатывается. Нажмите `D` или `P` и постройте новый план. |
| Идущая операция | `Esc`, затем `y` отменяет её. Завершённые шаги остаются выполненными, и wtui сообщает о них; это не откат и не выход из wtui. Затем повторите действие. |
| Непонятный сбой | `E` просит настроенный AI-провайдер (по умолчанию Codex, Claude при `ai.provider: claude`) объяснить причину и следующий шаг. Ничего не меняет. |
