# api_tests

Интеграционные тесты службы `ghreport` на pytest. Служба проверяется как чёрный ящик: тесты запускают её отдельным процессом и смотрят только на наблюдаемое поведение — отчёт, журнал `--trace`, коды возврата, файлы кэша и запросы, которые она отправила.

## Запуск

```bash
python3 -m venv .venv && .venv/bin/pip install -r api_tests/requirements.txt
GHREPORT_BIN=$PWD/service/ghreport GITHUB_API_BASE_URL=<адрес сервера> .venv/bin/pytest api_tests -q
```

Python ≥ 3.10, единственная зависимость — pytest ≥ 7. Полный прогон против тестового сервера занимает около 30 секунд; самые долгие тесты ждут паузы, которые задаёт сервер (`Retry-After: 1`, повторы после 5xx — 1 + 2 + 4 с).

### Переменные окружения

| Переменная | Обязательна | Назначение |
|---|---|---|
| `GHREPORT_BIN` | да | Абсолютный путь к собранной службе (или к `tools/docker_ghreport.sh`, чтобы проверить Docker-образ) |
| `GITHUB_API_BASE_URL` | да | Сервер, совместимый с контрактом из части 2 |
| `GITHUB_TOKEN` | нет | Токен для запросов к серверу — и самой службы, и тестов |
| `GHREPORT_TEST_REPOS` | нет | Репозитории для тестов через запятую, по умолчанию `stub/alpha`; используется первый. Нужен репозиторий хотя бы с двумя открытыми issue |

### Тестовый сервер

В `stub/` — заглушка эндпоинта `/repos/{owner}/{repo}/issues` на стандартной библиотеке: пагинация с `Link`, ETag и 304, 404 для неизвестных репозиториев, детерминированные данные (`stub/alpha` — 250 элементов, из них 62 pull request-а, пары с одинаковым `created_at`, кириллица, эмодзи и `<>&` в заголовках, удалённый автор; `stub/beta` — 3 issue; `stub/empty`).

```bash
cd api_tests && python3 -m stub.server --port 8080            # корректный сервер
cd api_tests && python3 -m stub.server --port 8080 --mutant skip_item
```

Против настоящего GitHub набор тоже работает (`GITHUB_API_BASE_URL=https://api.github.com GHREPORT_TEST_REPOS=cli/cli`), но нужен `GITHUB_TOKEN`: без токена лимит — 60 запросов в час, а набору нужно больше.

## Как устроен набор

Почти все нарушения контракта, которые набор обязан обнаруживать (пропуск элемента при пагинации, pull request без признака `pull_request`, 304 без `If-None-Match`, повреждённый JSON, неверное направление сортировки, отсутствие `Link`), — нарушения **сервера**, а не службы: служба добросовестно передаст в отчёт то, что получила. Поэтому проверки устроены в три слоя.

1. **Контракт сервера** (`test_contract.py`). Тесты сами читают выдачу (только GET и только по `GITHUB_API_BASE_URL`) и проверяют: тело — массив объектов нужного вида; сортировка по возрастанию; у элементов с `/pull/` в `html_url` есть ключ `pull_request`; при обходе мелкими страницами за последней страницей ничего нет (адрес следующей страницы тест собирает сам — служба так делать не должна); набор номеров не зависит от размера страницы; запрос без `If-None-Match` никогда не даёт 304; ETag есть, и запрос с ним даёт 304.
2. **Служба против эталона.** Ожидаемый отчёт не захардкожен: `support/reference.py` читает выдачу сервера сам и независимо реализует правила задания — фильтр PR, `--since`, дедупликацию, `--limit`, сортировку и маппинг полей. Отчёт службы сравнивается с ним поэлементно, поэтому набор работает с любыми данными.
3. **Сбои через прокси.** 401, 429, 5xx или битый JSON нельзя получить от произвольного сервера по требованию. `support/proxy.py` — локальный прокси на `127.0.0.1` между службой и `GITHUB_API_BASE_URL`, он пересылает запросы только туда и по правилам теста:
   - отвечает заданным статусом, заголовками и телом вместо пересылки;
   - портит тело или правит JSON-массив (добавить PR, поставить даты на границу `--since`, повторить элемент на следующей странице);
   - переписывает `per_page` (страница из одного элемента — любой репозиторий становится многостраничным) и адреса в `Link`, чтобы служба оставалась на прокси;
   - отдаёт настоящий репозиторий под другим именем — второй рабочий репозиторий при любых данных;
   - записывает запросы службы с заголовками и подменяет её `Authorization` на `GITHUB_TOKEN`, так что выдуманный тестом токен до сервера не доходит.

Требования к качеству набора:
- **детерминированность** — нет `sleep` для ожидания; время проверяется только снизу (служба не ответила раньше `Retry-After`), сверху запуск ограничен таймаутом процесса (120 с);
- **независимость** — у каждого теста свой каталог кэша (`tmp_path`), свой прокси; эталонные данные кэшируются на сессию, но только читаются;
- запуск службы — одна функция `run_ghreport` в `conftest.py`, тесты не вызывают `subprocess` сами.

## Покрытие требований

| Требование | Тесты |
|---|---|
| 1. Пагинация и `--limit` | `test_pagination.py`: ровно `limit` элементов при выдаче в несколько страниц, без повторов; переход строго по `Link: rel="next"`; после набора `limit` новые страницы не запрашиваются; повтор элемента на соседних страницах устраняется; `--limit` больше выдачи и значение по умолчанию 100 |
| 2. Pull request-ы | `test_filters.py`: настоящих PR нет в отчёте; добавленный прокси PR исключён; `"pull_request": null` тоже исключён |
| 3. `--since` | `test_filters.py`: только issue не раньше даты (дата — по медиане данных); граница ровно в полночь UTC, в том числе для времени с часовым поясом; дата позже всех issue даёт пустой список |
| 4. Кэш | `test_cache.py`: второй запуск с тем же кэшем — все записи журнала 304 и `from_cache: true`, отчёт совпадает; отправляется именно полученный ETag; свой пустой кэш запросов из кэша не даёт; каталог создаётся; токена в кэше нет |
| 5. Лимиты | `test_rate_limit.py`: 429 с `Retry-After` — пауза не короче указанной, повтор того же адреса с `attempt: 2`, отчёт как без сбоя; 403 с `X-RateLimit-Remaining: 0` и `X-RateLimit-Reset`; лимит на второй странице |
| 6. Ошибки | `test_errors.py`: 401 → код 3 и `{"error": "unauthorized"}`, в том числе на втором репозитории; 404 → код 1 и `not_found`; все репозитории с ошибкой → `all_failed`; битый JSON → `invalid_response`; 5xx повторяется, постоянный 5xx — 4 попытки и `request_failed`; некорректные аргументы → код 2 и пустой stdout |
| 7. Схема отчёта | `test_report_schema.py`: точный набор полей и типы, `generated_at` в UTC, сортировка по `created_at` и `number`, `count`, соответствие полей API, порядок и дедупликация `--repos` |
| 8. Журнал | `test_trace.py`: точный набор полей и типы; одна запись на каждый запрос (включая повтор); первый запрос — по контракту; заголовки запроса; токена и `Authorization` нет ни в журнале, ни в stdout, stderr и кэше |

## Самопроверка набора

`tools/mutation_check.py` прогоняет весь набор против сломанных серверов (мутанты заглушки) и сломанных служб (ghreport, собранный из исходников с одной внесённой ошибкой) и проверяет, что набор падает на каждой поломке и проходит на корректных вариантах:

```bash
python3 api_tests/tools/mutation_check.py          # нужен pytest и Go; несколько минут
```

Результат последнего прогона: набор проходит против корректного сервера и двух допустимых вариантов и падает на каждом из 7 нарушений контракта и каждой из 13 сломанных версий службы. Мутант `unsorted` сначала выжил — сервер по контракту и так отдаёт issue по порядку; это закрыл тест, в котором прокси переворачивает страницу.

### Broken servers

| Variant | What is broken | Expected | Failed tests |
|---|---|---|---|
| `correct` | the correct stub | suite passes | none |
| `skip_item` | the first item of every page after the first is missing | suite fails | `test_contract::test_pagination_is_complete`, `test_pagination::test_limit_spans_multiple_pages`, `test_pagination::test_stops_requesting_after_limit` and 2 more |
| `pr_without_key` | one pull request has no pull_request key | suite fails | `test_contract::test_pull_requests_carry_pull_request_key`, `test_filters::test_pull_requests_are_excluded` |
| `304_without_inm` | 304 on the first page even without If-None-Match | suite fails | `test_cache::test_second_run_uses_cache`, `test_cache::test_second_run_sends_the_stored_etag`, `test_cache::test_fresh_cache_dir_does_not_hit_cache` and 38 more |
| `corrupt_json` | the first page body is truncated JSON | suite fails | `test_cache::test_second_run_uses_cache`, `test_cache::test_second_run_sends_the_stored_etag`, `test_cache::test_fresh_cache_dir_does_not_hit_cache` and 37 more |
| `desc_sort` | items come newest first even with direction=asc | suite fails | `test_contract::test_listing_is_sorted_ascending` |
| `missing_link` | no Link header, even when more pages follow | suite fails | `test_contract::test_link_next_present_while_pages_remain`, `test_contract::test_pagination_is_complete`, `test_pagination::test_limit_spans_multiple_pages` and 3 more |
| `no_etag` | no ETag header and no conditional requests | suite fails | `test_cache::test_second_run_uses_cache`, `test_cache::test_cache_dir_is_created`, `test_cache::test_cache_does_not_contain_token` and 1 more |
| `duplicate_item` | every page after the first repeats the last item of the previous one | suite passes | none |
| `link_omitted_on_304` | 304 responses carry no Link header | suite passes | none |

### Broken services

| Variant | What is broken | Expected | Failed tests |
|---|---|---|---|
| `keeps_pull_requests` | pull requests are not filtered out | suite fails | `test_filters::test_pull_requests_are_excluded`, `test_filters::test_injected_pull_request_is_excluded`, `test_filters::test_pull_request_key_with_null_value_is_excluded` and 5 more |
| `keeps_duplicates` | repeated numbers are not removed | suite fails | `test_pagination::test_duplicates_across_pages_are_removed` |
| `ignores_since` | --since is not applied | suite fails | `test_filters::test_since_keeps_issues_created_on_or_after_date`, `test_filters::test_since_boundary_is_midnight_utc`, `test_filters::test_since_after_all_issues_returns_empty` |
| `unsorted` | issues are not sorted | suite fails | `test_report_schema::test_issues_are_sorted_even_when_a_page_is_not` |
| `limit_off_by_one` | --limit lets one issue too many through | suite fails | `test_pagination::test_limit_spans_multiple_pages`, `test_pagination::test_stops_requesting_after_limit`, `test_pagination::test_default_limit_is_100` and 3 more |
| `first_page_only` | Link rel=next is not followed | suite fails | `test_filters::test_pull_requests_are_excluded`, `test_filters::test_injected_pull_request_is_excluded`, `test_filters::test_since_keeps_issues_created_on_or_after_date` and 8 more |
| `no_conditional_requests` | If-None-Match is never sent | suite fails | `test_cache::test_second_run_uses_cache`, `test_cache::test_second_run_sends_the_stored_etag` |
| `no_rate_limit_wait` | rate-limit pauses are skipped | suite fails | `test_rate_limit::test_waits_retry_after_on_429_and_succeeds`, `test_rate_limit::test_waits_until_reset_on_403_with_zero_remaining` |
| `no_5xx_retry` | 5xx responses are not retried | suite fails | `test_errors::test_server_error_is_retried`, `test_errors::test_persistent_server_error_fails_repo`, `test_trace::test_trace_has_one_record_per_request` |
| `attempt_always_1` | the trace numbers every attempt 1 | suite fails | `test_errors::test_server_error_is_retried`, `test_errors::test_persistent_server_error_fails_repo`, `test_rate_limit::test_waits_retry_after_on_429_and_succeeds` |
| `token_in_trace` | the token ends up in the trace | suite fails | `test_pagination::test_follows_link_next_exactly`, `test_trace::test_token_never_leaks` |
| `unauthorized_not_fatal` | 401 is reported like any other failure | suite fails | `test_errors::test_401_is_fatal_with_exit_code_3`, `test_errors::test_401_on_a_later_repository_discards_the_report` |
| `partial_failure_exits_0` | exit code 0 although a repository failed | suite fails | `test_errors::test_404_marks_repo_and_exit_code_1`, `test_errors::test_invalid_json_marks_repo`, `test_errors::test_persistent_server_error_fails_repo` and 1 more |

## Прогон против Docker-образа

`tools/docker_ghreport.sh` запускает образ так, будто это бинарь: монтирует каталоги `--cache-dir`, `--trace`, `--out` по тем же путям и делает адрес сервера на хосте доступным из контейнера. Это проверяет, что контейнер обращается именно по `GITHUB_API_BASE_URL`.

```bash
docker build -t ghreport service/
GHREPORT_BIN=$PWD/api_tests/tools/docker_ghreport.sh GITHUB_API_BASE_URL=http://127.0.0.1:8080 \
  .venv/bin/pytest api_tests -q
```
