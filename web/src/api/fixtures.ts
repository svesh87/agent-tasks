// Fake data for the mockup: a fictional team ("acme") and its owner. Times are relative to
// page load so "2 hours ago" stays true.
import type { Author, By, ClosedReason, CommentState, DocKind, JournalKind, PlaceRole, RoundState, TaskStatus } from './types';

const NOW = Date.now();
/** ISO time `m` minutes before page load. */
export const ago = (m: number) => new Date(NOW - m * 60_000).toISOString();
const H = 60;
const D = 24 * H;

export interface FxReply {
  author: 'agent' | 'owner';
  body: string;
  at: number;
}
export interface FxComment {
  /** Exact markdown text in the round's revision; omitted for a general comment. */
  quote?: string;
  occurrence?: number;
  body: string;
  state: CommentState;
  at: number;
  replies?: FxReply[];
}
export interface FxRound {
  revision: number;
  state: RoundState;
  summary?: string;
  at: number;
  submittedAt?: number;
  addressedAt?: number;
  addressedMessage?: string;
  addressedIn?: number;
  comments: FxComment[];
}
export interface FxRevision {
  body: string;
  author: Author;
  message?: string;
  at: number;
}
export interface FxDoc {
  slug: string;
  kind: DocKind;
  title: string;
  revisions: FxRevision[];
  /** State of plan items in the latest revision, keyed by the start of the item text. */
  items?: Record<string, { done: boolean; by?: By; notes?: [string, number, By?][] }>;
  rounds?: FxRound[];
}
export interface FxJournal {
  kind: JournalKind;
  body: string;
  source?: string;
  at: number;
}
export interface FxTask {
  id: string;
  title: string;
  summary?: string;
  status: TaskStatus;
  waiting_on?: string;
  closed_reason?: ClosedReason;
  archivedAt?: number;
  tags: string[];
  /** [place key, role, note?] */
  places?: [string, PlaceRole, string?][];
  createdAt: number;
  updatedAt: number;
  docs?: FxDoc[];
  journal?: FxJournal[];
}

// ---------------------------------------------------------------- places and machines

export const INFRA = 'github.com/acme/infra';
export const AGENT_TASKS = 'github.com/acme/agent-tasks';
export const DEVENV = 'github.com/acme/devenv';
export const BILLING = 'gitlab.example.com/platform/billing';
export const CI_TEMPLATES = 'gitlab.example.com/platform/ci-templates';
export const DRILL = 'laptop:/home/alex/experiments/restore-drill';
export const AGENT_WORK = 'laptop:/home/alex/agent-work';

/** Places with their known locations: [key, label?, [machine, path, seenMinutesAgo][]] */
export const fxPlaces: [string, string | undefined, [string, string, number][]][] = [
  [INFRA, undefined, [
    ['laptop', '/home/alex/src/infra', 35],
    ['macbook', '/Users/alex/src/infra', 3 * D],
    ['buildbox', '/srv/work/infra', 5 * D],
  ]],
  [AGENT_TASKS, undefined, [['laptop', '/home/alex/src/agent-tasks', 12]]],
  [DEVENV, undefined, [['laptop', '/home/alex/src/devenv', 10 * D], ['buildbox', '/srv/work/devenv', 10 * D]]],
  [BILLING, undefined, [['work-laptop', '/Users/alex/work/billing', 1 * D]]],
  [CI_TEMPLATES, undefined, []],
  [DRILL, undefined, [['laptop', '/home/alex/experiments/restore-drill', 2 * H]]],
  [AGENT_WORK, undefined, [['laptop', '/home/alex/agent-work', 1 * D]]],
  // More places, as a real user has (the picker must stay usable with 20+).
  ...[
    'github.com/acme/webapp',
    'github.com/acme/mobile-app',
    'github.com/acme/design-system',
    'github.com/acme/docs-site',
    'github.com/acme/terraform-modules',
    'github.com/acme/agent-rules',
    'github.com/acme/dotfiles',
    'github.com/acme/mcp-servers',
    'github.com/acme/release-tools',
    'gitlab.example.com/platform/payments',
    'gitlab.example.com/platform/infra-terraform',
    'gitlab.example.com/platform/helm-charts',
    'gitlab.example.com/data/etl-jobs',
    'gitlab.example.com/data/dashboards',
    'laptop:/home/alex/experiments/llm-evals',
    'laptop:/home/alex/scratch/photos-dedupe',
  ].map((k): [string, string | undefined, [string, string, number][]] => [
    k,
    undefined,
    [['laptop', k.includes(':/') ? k.slice(7) : `/home/alex/src/${k.split('/').pop()}`, 9 * D]],
  ]),
];

export interface FxTodo {
  text: string;
  note?: string;
  state: 'open' | 'done' | 'dropped' | 'in_task';
  task?: string;
  origin?: string;
  places: string[];
  tags?: string[];
  source?: string;
  at: number;
  closedAt?: number;
}

/** Todos D-1…D-14 in order. */
export const fxTodos: FxTodo[] = [
  { text: 'Проверить, что общий бэкап /srv не копирует /srv/backup/wiki второй раз', places: [INFRA], origin: 'T-42', tags: ['topic:backup'], state: 'open', source: 'laptop · claude-code', at: 3 * H },
  { text: 'Rotate the DNS provider API token used by Terraform', places: [INFRA], tags: ['security'], state: 'open', source: 'owner', at: 2 * D },
  { text: 'Add `--json` output to the export command', places: [AGENT_TASKS], origin: 'T-45', state: 'open', source: 'laptop · codex', at: 5 * H },
  {
    text: 'Попробовать синхронизацию ~/agent-work между ноутбуком и макбуком',
    note: 'Конфликты при одновременной правке? Проверить на двух сессиях сразу.',
    places: [],
    tags: ['area:personal'],
    state: 'open',
    source: 'owner',
    at: 1 * D + 3 * H,
  },
  { text: 'Read the PostgreSQL release notes for breaking changes', places: [INFRA], tags: ['topic:postgres'], state: 'in_task', task: 'T-33', source: 'owner', at: 20 * D },
  { text: 'Grafana: dashboard with the age of every backup', places: [INFRA], origin: 'T-42', tags: ['topic:monitoring', 'topic:backup'], state: 'open', source: 'laptop · claude-code', at: 6 * H },
  { text: 'Заказать второй диск того же объёма', places: [], tags: ['area:personal'], state: 'open', source: 'owner', at: 4 * D },
  { text: 'Warm-up job for the kaniko cache', places: [BILLING, CI_TEMPLATES], origin: 'T-41', tags: ['topic:ci', 'area:work'], state: 'done', source: 'work laptop · claude-code', at: 6 * D, closedAt: 2 * D },
  { text: 'Dark theme for the diff view', places: [AGENT_TASKS], tags: ['topic:frontend'], state: 'done', source: 'owner', at: 1 * D, closedAt: 3 * H },
  { text: 'Try a hosted wiki’s MCP for the docs folder', places: [], origin: 'T-36', tags: ['topic:agent-tasks'], state: 'dropped', source: 'owner', at: 7 * D, closedAt: 3 * D },
  { text: 'Скрипт чистки ~/agent-work по тому же сроку, что архив', places: [AGENT_WORK], state: 'open', source: 'laptop · claude-code', at: 1 * D },
  {
    text: 'Write down the NFS `all_squash` pitfall in docs/restore.md',
    note: 'The backup job runs as root; `root_squash` breaks the first dump with *permission denied*.',
    places: [INFRA, DRILL],
    origin: 'T-42',
    tags: ['topic:wiki'],
    state: 'open',
    source: 'laptop · claude-code',
    at: 2 * H,
  },
  { text: 'Check SMART of the old SATA disk before reuse', places: [INFRA], tags: ['topic:monitoring'], state: 'in_task', task: 'T-39', source: 'owner', at: 5 * D },
  { text: 'Ask the team about release freeze dates', places: [BILLING], tags: ['area:work'], state: 'open', source: 'owner', at: 2 * D },
];

// ---------------------------------------------------------------- T-42: the showcase task

const wikiIntro = `# План: бэкапы вики на NFS

Цель — чтобы дамп базы вики каждый день уходил на сетевую шару, а восстановление было
проверено, а не предполагалось.

## Как сейчас

- Вики работает в VM, дампы складываются внутрь VM, на тот же диск.
- Шара \`/srv/backup/wiki\` уже экспортируется по NFS, но VM к ней не подключена.
`;

const wikiPlanR1 = `${wikiIntro}
## Шаги

- [ ] Экспорт NFS только для адреса VM, \`rw,root_squash\`
- [ ] Подключить шару в VM: \`/etc/fstab\`, точка монтирования \`/mnt/backup\`
- [ ] Расписание: ежедневно в 03:30, хранить 7 копий
- [ ] Проверить восстановление на чистой VM
- [ ] Записать порядок восстановления в docs/restore.md

## Риски

Если NFS недоступен в момент бэкапа, дамп пишется локально и задание молча продолжает.
`;

const wikiSize = `
## Объём

| Что | Размер | За 7 дней |
|---|---:|---:|
| Дамп базы | ~1,2 ГБ | ~8,5 ГБ |
| Вложения | ~380 МБ | входят в дамп |
| Свободно на \`/srv\` | 2,1 ТБ | — |
`;

const wikiPlanR2 = `${wikiIntro}${wikiSize}
## Шаги

- [ ] Экспорт NFS только для адреса VM, \`rw,all_squash,anonuid=1300,anongid=1300\`
- [ ] Подключить шару в VM: \`/etc/fstab\`, точка монтирования \`/mnt/backup\`
- [ ] Расписание: ежедневно в 03:30, хранить 7 копий
- [ ] Алерт, если свежего бэкапа на шаре нет дольше 26 часов
- [ ] Проверить восстановление на чистой VM
- [ ] Записать порядок восстановления в docs/restore.md

Строка экспорта, которую добавит роль:

\`\`\`
/srv/backup/wiki  10.0.0.50(rw,sync,all_squash,anonuid=1300,anongid=1300)
\`\`\`

## Проверки

- \`showmount -e\` с VM видит только \`/srv/backup/wiki\`.
- После первого бэкапа на шаре лежит \`*.dump\` с владельцем 1300.
`;

const wikiPlanR3 = `${wikiIntro}${wikiSize}
## Шаги

- [x] Экспорт NFS только для адреса VM, \`rw,all_squash,anonuid=1300,anongid=1300\`
- [ ] Подключить шару в VM: \`/etc/fstab\`, точка монтирования \`/mnt/backup\`
- [ ] Расписание: ежедневно в 02:00, хранить 7 копий
- [ ] Алерт в Grafana, если свежего бэкапа на шаре нет дольше 26 часов
- [ ] Проверить восстановление во временной VM \`wiki-restore-test\`, затем удалить её
- [ ] Записать порядок восстановления в \`docs/restore.md\`

### Как делается экспорт

Роль \`nfs_exports\` добавляет строку и перечитывает таблицу без рестарта сервиса:

\`\`\`yaml
nfs_exports:
  - path: /srv/backup/wiki
    clients: 10.0.0.50
    options: rw,sync,all_squash,anonuid=1300,anongid=1300
\`\`\`

> Дамп шифруется ключом команды. Без ключа восстановление невозможно, поэтому ключ лежит в
> менеджере паролей, а не только в VM.

### Алерт

1. Экспортёр \`textfile\` раз в час пишет возраст самого свежего \`*.dump\`.
2. Правило в Grafana: возраст больше 26 часов — уведомление в чат дежурных.
   - тихие часы не нужны: алерт редкий;
   - повтор раз в 12 часов, пока не исправлено.

## Проверки

- \`showmount -e\` с VM видит только \`/srv/backup/wiki\`.
- После первого бэкапа на шаре лежит \`*.dump\` с владельцем 1300.
- Временная VM поднимается из дампа, вики открывается, поиск работает.

## Решения по умолчанию

- Хранить 7 копий: ~8,5 ГБ, места с запасом. Можно поменять на ревью.
- Имя задания в cron — \`wiki-backup\`.
`;

const wikiResearch = `# Research: database dumps to an NFS share

\`pg_dump\` can write straight to a mounted share. Both **NFS** and **CIFS/SMB** work; NFS is
simpler here because the storage server already exports \`/srv/backup\`.

## Findings

- The backup job runs as root; \`root_squash\` maps it to \`nobody\` and the first dump fails
  with *permission denied*. \`all_squash\` with a fixed uid works.
- With \`-Fc\` the dump is compressed; writing to the share directly needs no local space.
- If the mount is unavailable, the job **fails** (it is not silently written locally — that was
  an assumption in plan r1), as long as the mount point is not a plain directory.

## Sizes measured

| Date | Dump | Duration |
|---|---:|---:|
| 2026-11-10 | 1.17 GB | 5 min 40 s |
| 2026-11-11 | 1.18 GB | 5 min 52 s |
| 2026-11-12 | 1.21 GB | 6 min 05 s |

## Sources

- PostgreSQL docs, *pg_dump*.
- \`nfs(5)\`: \`hard\` vs \`soft\` mounts and what a failed write looks like.
`;

const wikiRunbook = `# Восстановление вики из дампа (черновик)

Импортировано из старой заметки владельца.

1. Создать VM из того же образа и той же версии вики.
2. Остановить вики, создать пустую базу.
3. \`pg_restore -d wiki /mnt/backup/wiki-<дата>.dump\` (ключ шифрования — в менеджере паролей).
4. Запустить вики, дождаться переиндексации — около 10 минут.

После восстановления проверить вложения: путь к хранилищу файлов мог смениться.
`;

const t42: FxTask = {
  id: 'T-42',
  title: 'Бэкапы вики на NFS и проверка восстановления',
  summary: 'Ежедневный дамп базы вики на сетевую шару, алерт при пропуске и учения восстановления во временной VM.',
  status: 'active',
  tags: ['topic:wiki', 'topic:backup', 'area:work'],
  places: [[INFRA, 'primary'], [DRILL, 'touched', 'Test VM configs for the restore drill']],
  createdAt: 5 * D + 3 * H,
  updatedAt: 35,
  docs: [
    {
      slug: 'plan',
      kind: 'plan',
      title: 'План',
      revisions: [
        { body: wikiPlanR1, author: 'agent', at: 5 * D, message: 'Первый вариант' },
        { body: wikiPlanR2, author: 'agent', at: 3 * D + 2 * H, message: 'Учёл раунд ревью 1' },
        { body: wikiPlanR3, author: 'agent', at: 6 * H, message: 'Учёл раунд ревью 2: время, алерт через Grafana' },
      ],
      items: {
        'Экспорт NFS': {
          done: true,
          notes: [
            ['`make deploy` применил роль `nfs_exports`, повторный прогон — пустой diff.', 4 * H],
            ['`showmount -e` с VM видит только `/srv/backup/wiki`.', 3 * H + 40],
          ],
        },
        'Подключить шару': {
          done: true,
          by: 'owner',
          notes: [
            ['Шара смонтирована, тестовый дамп 1,18 ГБ за 5 мин 52 с.', 2 * H],
            ['Проверил после перезагрузки VM — шара на месте.', 90, 'owner'],
          ],
        },
        'Расписание': {
          done: false,
          notes: [
            ['Ждёт владельца: окно бэкапов согласуется с командой.', 50],
            ['Согласую сам вечером.', 20, 'owner'],
          ],
        },
        'Алерт в Grafana': { done: false },
        'Проверить восстановление': { done: false },
        'Записать порядок': { done: false },
      },
      rounds: [
        {
          revision: 1,
          state: 'addressed',
          summary: 'В целом да. Две правки и вопрос про место.',
          at: 4 * D + 20 * H,
          submittedAt: 4 * D + 19 * H,
          addressedAt: 3 * D + 2 * H,
          addressedMessage: 'План r2: all_squash, оценка места, алерт вместо фразы в рисках.',
          addressedIn: 2,
          comments: [
            {
              quote: 'rw,root_squash',
              body: 'Почему не `all_squash` с отдельным uid? Задание работает от root.',
              state: 'resolved',
              at: 4 * D + 20 * H,
              replies: [
                {
                  author: 'agent',
                  body: 'С `root_squash` первый дамп падает с *permission denied*. Проверил `all_squash,anonuid=1300` на тестовой шаре — работает. Поменял в r2.',
                  at: 3 * D + 3 * H,
                },
              ],
            },
            {
              quote: 'Если NFS недоступен в момент бэкапа, дамп пишется локально и задание молча продолжает.',
              body: 'Это не риск, а дыра. Нужен алерт, а не фраза в рисках. И проверь, правда ли пишет локально.',
              state: 'outdated',
              at: 4 * D + 20 * H,
              replies: [
                {
                  author: 'agent',
                  body: 'Проверил: локально не пишет, задание падает (см. research). Раздел убрал, добавил шаг с алертом.',
                  at: 3 * D + 3 * H,
                },
              ],
            },
            {
              body: 'И добавь, сколько места это займёт за неделю.',
              state: 'resolved',
              at: 4 * D + 19 * H,
              replies: [
                { author: 'agent', body: 'Добавил таблицу «Объём»: ~1,2 ГБ на копию, ~8,5 ГБ за 7 дней.', at: 3 * D + 3 * H },
              ],
            },
          ],
        },
        {
          revision: 2,
          state: 'addressed',
          at: 1 * D + 4 * H,
          submittedAt: 1 * D + 2 * H,
          addressedAt: 6 * H,
          addressedMessage: 'План r3: 02:00, алерт в Grafana, временная VM для учений.',
          addressedIn: 3,
          comments: [
            {
              quote: 'ежедневно в 03:30',
              body: 'В 03:30 стартует общий бэкап `/srv`. Сдвинь на 02:00, чтобы дамп вики попал в ту же ночь.',
              state: 'resolved',
              at: 1 * D + 4 * H,
              replies: [
                {
                  author: 'agent',
                  body: 'Сдвинул на 02:00. Дамп идёт ~6 минут, к 03:30 давно закончен.',
                  at: 6 * H,
                },
              ],
            },
            {
              quote: 'Проверить восстановление',
              body: 'Чистую VM мы не держим. Восстанавливай во временную и удаляй её после проверки.',
              state: 'open',
              at: 1 * D + 3 * H,
              replies: [
                {
                  author: 'agent',
                  body: 'Уточнил шаг в r3: временная VM `wiki-restore-test`, удаляю после проверки. Оставляю открытым до самих учений.',
                  at: 6 * H,
                },
                { author: 'owner', body: 'Ок. Удаление — только после моего «да».', at: 5 * H },
              ],
            },
            {
              body: 'Алерт — через то, что уже есть (Grafana), а не новым сервисом.',
              state: 'resolved',
              at: 1 * D + 2 * H,
              replies: [{ author: 'agent', body: 'Сделал через textfile-экспортёр и правило в Grafana.', at: 6 * H }],
            },
          ],
        },
        {
          revision: 3,
          state: 'draft',
          at: 40,
          comments: [
            {
              quote: 'дольше 26 часов',
              body: 'Почему 26, а не 25? Бэкап ежедневный, лишний час запаса не нужен.',
              state: 'open',
              at: 40,
            },
            {
              body: 'В шаге про документацию упомяни, где лежит ключ шифрования дампа.',
              state: 'open',
              at: 30,
            },
          ],
        },
      ],
    },
    {
      slug: 'research',
      kind: 'research',
      title: 'Research: database dumps to an NFS share',
      revisions: [{ body: wikiResearch, author: 'agent', at: 3 * D + 5 * H }],
    },
    {
      slug: 'restore-runbook',
      kind: 'notes',
      title: 'Порядок восстановления',
      revisions: [{ body: wikiRunbook, author: 'import', at: 4 * D, message: 'Импорт из notes/wiki-restore.md' }],
    },
  ],
  journal: [
    {
      kind: 'context',
      at: 35,
      source: 'laptop · claude-code',
      body: `Где стоим:
- шаги 1–2 плана сделаны, экспорт и шара в VM работают;
- шаг 3 (расписание 02:00) ждёт владельца — окно согласуется с командой;
- владелец пишет раунд 3 (черновик), не начинать шаг 4 до него.

Дальше: алерт (textfile-экспортёр + правило Grafana), потом учения во временной VM.`,
    },
    { kind: 'log', at: 2 * H, source: 'laptop · claude-code', body: 'Смонтировал шару в VM, сделал тестовый дамп (1,18 ГБ, 5 мин 52 с).' },
    { kind: 'log', at: 4 * H, source: 'laptop · claude-code', body: '`make check` → `make deploy` (роль `nfs_exports`), повторный прогон — пустой diff.' },
    { kind: 'decision', at: 5 * H, source: 'owner', body: 'Временную VM `wiki-restore-test` удалять только после «да» владельца.' },
    { kind: 'log', at: 6 * H, source: 'laptop · claude-code', body: 'Записал план r3, ответил на раунд 2, раунд закрыт.' },
    { kind: 'decision', at: 1 * D + 2 * H, source: 'owner', body: 'Алерт — через Grafana, не новым сервисом (раунд 2).' },
    {
      kind: 'context',
      at: 3 * D,
      source: 'buildbox · runner',
      body: 'План r2 записан, ждёт ревью владельца. Research приложен: при недоступном NFS задание падает, локально не пишет.',
    },
    { kind: 'decision', at: 3 * D + 3 * H, source: 'owner', body: '`all_squash,anonuid=1300` вместо `root_squash` (раунд 1).' },
  ],
};

// ---------------------------------------------------------------- T-45: English, submitted round

const atPlanR1 = `# Plan: agent-tasks backend MVP

Go + SQLite server implementing CONTRACT.md v0: MCP over Streamable HTTP and a REST API for the
web UI.

## Steps

- [ ] Schema and migrations (tasks, documents, revisions, items, rounds, comments, journal)
- [ ] FTS5 index with Snowball stemming done in the app
- [ ] MCP tools from the contract
- [ ] REST endpoints for the web UI
- [ ] Export and import of markdown
- [ ] Retention job for archived tasks
`;

const atPlanR2 = `# Plan: agent-tasks backend MVP

Go + SQLite server implementing CONTRACT.md v0: MCP over Streamable HTTP and a REST API for the
web UI. One binary, one database file, the web UI embedded.

## Steps

- [ ] Schema and migrations (tasks, documents, revisions, items, rounds, comments, journal)
- [ ] FTS5 index with Snowball stemming done in the app (Russian and English)
- [ ] Re-anchoring of comments and matching of plan items on a new revision
- [ ] MCP tools from the contract, bearer tokens from config
- [ ] REST endpoints for the web UI
- [ ] Export and import of markdown
- [ ] Retention job for archived tasks
- [ ] Dockerfile and a CI build of the image (no push)

## Libraries

| Need | Choice | Why |
|---|---|---|
| SQLite | \`modernc.org/sqlite\` | pure Go, no cgo, FTS5 built in |
| MCP | \`github.com/modelcontextprotocol/go-sdk\` | official SDK, Streamable HTTP |
| Stemming | \`github.com/kljensen/snowball\` | Russian and English stemmers |
| HTTP | standard library | the router in 1.22+ is enough |

## Defaults

- Database path \`/data/agent-tasks.db\`, WAL mode.
- Tokens in a YAML config, one per client, compared in constant time.
`;

const atResearch = `# Research: full-text search for Russian in SQLite

FTS5 has no Russian stemmer. Options:

1. **Stem in the app.** Index a second column with stemmed words; stem the query the same way.
   Snippets come from the original column via a contentless or external-content table.
2. **ICU tokenizer.** Needs cgo and an ICU build; still no stemming, only word breaking.
3. **Trigram tokenizer.** Matches substrings, no morphology; index size grows ~3×.

Option 1 is the only one that finds «бэкапы» for the query «бэкап» without cgo.

\`\`\`sql
CREATE VIRTUAL TABLE search USING fts5(
  title, body,           -- stemmed text
  task_id UNINDEXED, kind UNINDEXED, ref UNINDEXED,
  tokenize = 'unicode61 remove_diacritics 2'
);
\`\`\`

Mixed-language text: run both stemmers and keep the shorter stem when they differ? That breaks
English words like *news* → *new*. Detect script per word instead: Cyrillic → Russian, Latin →
English.
`;

const t45: FxTask = {
  id: 'T-45',
  title: 'agent-tasks: backend MVP in Go + SQLite',
  summary: 'Server side of agent-tasks per CONTRACT.md v0. Web UI mockups are a separate task (T-46).',
  status: 'active',
  tags: ['topic:agent-tasks', 'topic:mcp', 'area:personal'],
  places: [[AGENT_TASKS, 'primary'], [INFRA, 'touched', 'Deployment role later']],
  createdAt: 2 * D,
  updatedAt: 20,
  docs: [
    {
      slug: 'plan',
      kind: 'plan',
      title: 'Plan',
      revisions: [
        { body: atPlanR1, author: 'agent', at: 2 * D },
        { body: atPlanR2, author: 'agent', at: 1 * D, message: 'Libraries, re-anchoring step, Dockerfile' },
      ],
      items: {
        'Schema and migrations': { done: true, notes: [['Migrations 0001–0004, `go test ./internal/store` passes.', 8 * H]] },
        'FTS5 index': { done: true, notes: [['Script detection per word works for mixed text.', 3 * H]] },
        'Re-anchoring': { done: false, notes: [['Exact pass done; fuzzy pass in progress.', 25]] },
      },
      rounds: [
        {
          revision: 1,
          state: 'addressed',
          at: 1 * D + 6 * H,
          submittedAt: 1 * D + 5 * H,
          addressedAt: 1 * D,
          addressedIn: 2,
          addressedMessage: 'r2 adds libraries and Dockerfile.',
          comments: [
            {
              quote: 'Retention job for archived tasks',
              body: 'Also a Dockerfile and CI image build — but no push until I say so.',
              state: 'resolved',
              at: 1 * D + 6 * H,
              replies: [{ author: 'agent', body: 'Added as the last step; CI builds without pushing.', at: 1 * D }],
            },
          ],
        },
      ],
    },
    {
      slug: 'research',
      kind: 'research',
      title: 'Research: Russian full-text search in SQLite',
      revisions: [{ body: atResearch, author: 'agent', at: 1 * D + 8 * H }],
      rounds: [
        {
          revision: 1,
          state: 'submitted',
          summary: 'Option 1 is fine. Answer the two questions before the FTS step.',
          at: 2 * H,
          submittedAt: 90,
          comments: [
            {
              quote: 'Detect script per word instead',
              body: 'What about words like `nfs_exports` or `T-42`? Make sure IDs and identifiers are searchable as typed.',
              state: 'open',
              at: 2 * H,
            },
            {
              quote: 'index size grows ~3×',
              body: 'Measured or guessed? If guessed, say so.',
              state: 'open',
              at: 2 * H,
            },
            {
              body: 'Search must also cover comments and the journal, not only documents.',
              state: 'open',
              at: 95,
            },
          ],
        },
      ],
    },
  ],
  journal: [
    { kind: 'context', at: 20, source: 'laptop · codex', body: 'Store and FTS done. Next: fuzzy re-anchoring, then MCP tools. Owner submitted a review of the research — answer it first.' },
    { kind: 'log', at: 3 * H, source: 'laptop · codex', body: 'FTS5 with app-side stemming; 41 tests pass.' },
    { kind: 'decision', at: 1 * D, source: 'owner', body: 'CI builds the image but does not push until the owner says so.' },
  ],
};

// ---------------------------------------------------------------- the rest of the list

const t46Plan = `# Plan: web UI mockups

- [x] Fake data layer with the contract's model
- [x] Task list with search, status and tag filters
- [ ] Document review: selection → comment, draft round, submit
- [ ] Revision history and diff
- [ ] Phone layout and dark theme
`;

const ciPlan = `# План: CI биллинга на новые раннеры

Старые раннеры на shell-исполнителе выводятся из эксплуатации 1 декабря.

## Шаги

- [ ] Инвентаризация джоб: какие требуют docker-in-docker
- [ ] Шаблон \`ci-templates/build.yml\` с kaniko вместо dind
- [ ] Перевести \`billing-api\`
- [ ] Перевести \`billing-worker\`
- [ ] Перевести \`billing-reports\`
- [ ] Кэш Go-модулей на новых раннерах
- [ ] Снять теги старых раннеров с проектов
- [ ] Прогнать релизную ветку целиком
- [ ] Письмо команде о переезде
`;

const accessPlan = `# Plan: an auth proxy in front of /mcp

The web UI is behind the proxy with the owner's login. \`/mcp\` is called by agents and cannot
do an interactive login.

- [ ] Separate proxy application for \`tasks.example.com/mcp\` with a service-token policy
- [ ] One service token per machine (laptop, macbook, buildbox, CI)
- [ ] Clients send the proxy's service-token headers plus the app's bearer token
- [ ] Rotate tokens yearly; reminder in this task

Waiting for the owner to review before any change to the proxy.
`;

const wikiStoreResearch = `# A hosted wiki as the task store

The wiki has a built-in MCP server and comments anchored to quotes.

**Stopped:** Russian search matches exact forms only; comments are not tied to revisions; it
needs Postgres, Redis and OIDC. Decided to write our own server instead (T-45).
`;

const runnerNotes = `# Профили раннера агентов

- **Ручной**: скилл \`plan\`, вопросы через инструмент вопросов раннера.
- **Авто**: правила конкретной репы, без планирования.

Вернуться после MVP сервера задач.
`;

export const fixtures: FxTask[] = [
  t42,
  t45,
  {
    id: 'T-46',
    title: 'agent-tasks: web UI mockups',
    status: 'active',
    tags: ['topic:agent-tasks', 'topic:frontend', 'area:personal'],
    places: [[AGENT_TASKS, 'primary']],
    createdAt: 1 * D,
    updatedAt: 12,
    docs: [
      {
        slug: 'plan',
        kind: 'plan',
        title: 'Plan',
        revisions: [{ body: t46Plan, author: 'agent', at: 1 * D }],
        items: {
          'Fake data layer': { done: true },
          'Task list': { done: true },
        },
      },
    ],
    journal: [{ kind: 'context', at: 12, source: 'laptop · claude-code', body: 'List and task page done; review screen in progress.' }],
  },
  {
    id: 'T-44',
    title: 'Auth proxy: service tokens for /mcp',
    status: 'waiting',
    waiting_on: 'owner review',
    tags: ['topic:proxy', 'topic:agent-tasks', 'security'],
    places: [[INFRA, 'primary']],
    createdAt: 1 * D + 2 * H,
    updatedAt: 3 * H,
    docs: [{ slug: 'plan', kind: 'plan', title: 'Plan', revisions: [{ body: accessPlan, author: 'agent', at: 3 * H }] }],
    journal: [{ kind: 'context', at: 3 * H, source: 'laptop · claude-code', body: 'Plan written, link sent. Nothing changed in the proxy.' }],
  },
  {
    id: 'T-41',
    title: 'Переезд CI биллинга на новые раннеры',
    summary: 'Старые shell-раннеры выключают 1 декабря. Перевести три проекта на kaniko.',
    status: 'active',
    tags: ['topic:ci', 'area:work', 'urgent'],
    places: [[BILLING, 'primary'], [CI_TEMPLATES, 'touched', 'Shared build template']],
    createdAt: 9 * D,
    updatedAt: 1 * D + 1 * H,
    docs: [
      {
        slug: 'plan',
        kind: 'plan',
        title: 'План',
        revisions: [{ body: ciPlan, author: 'agent', at: 8 * D }],
        items: {
          'Инвентаризация': { done: true, notes: [['dind нужен 4 джобам из 23.', 7 * D]] },
          'Шаблон': { done: true },
          'Перевести `billing-api`': { done: true },
          'Перевести `billing-worker`': { done: true, notes: [['Тесты интеграции медленнее на 40 с — кэш не прогрет.', 2 * D]] },
        },
      },
    ],
    journal: [
      { kind: 'context', at: 1 * D + 1 * H, source: 'work laptop · claude-code', body: 'Осталось: reports, кэш модулей, снять старые теги, релизный прогон, письмо.' },
    ],
  },
  {
    id: 'T-40',
    title: 'Продлить домен и проверить автопродление',
    status: 'waiting',
    waiting_on: 'ответ регистратора',
    tags: ['topic:dns', 'area:personal'],
    createdAt: 6 * D,
    updatedAt: 2 * D,
    journal: [{ kind: 'log', at: 2 * D, source: 'owner', body: 'Написал регистратору: автопродление не включается из панели.' }],
  },
  {
    id: 'T-39',
    title: 'Grafana: alert on SMART errors of NVMe disks',
    summary: 'Read `smartctl` via node-exporter textfile, alert on media errors and spare below 20 %.',
    status: 'active',
    tags: ['topic:monitoring'],
    places: [[INFRA, 'primary']],
    createdAt: 4 * D,
    updatedAt: 2 * D + 5 * H,
  },
  {
    id: 'T-43',
    title: 'Раннер агентов: два профиля — ручной и авто',
    status: 'later',
    tags: ['topic:agents', 'topic:agent-tasks', 'area:personal'],
    places: [[INFRA, 'primary']],
    createdAt: 1 * D + 5 * H,
    updatedAt: 1 * D + 5 * H,
    docs: [{ slug: 'notes', kind: 'notes', title: 'Заметки', revisions: [{ body: runnerNotes, author: 'agent', at: 1 * D + 5 * H }] }],
  },
  {
    id: 'T-33',
    title: 'Обновить PostgreSQL до следующей мажорной версии',
    summary: 'Прочитать заметки о ломающих изменениях; дамп, новая база, восстановление.',
    status: 'later',
    tags: ['topic:postgres', 'area:work'],
    places: [[INFRA, 'primary']],
    createdAt: 20 * D,
    updatedAt: 14 * D,
  },
  {
    id: 'T-31',
    title: 'Draw the network diagram',
    status: 'later',
    tags: ['docs', 'area:personal'],
    createdAt: 25 * D,
    updatedAt: 25 * D,
  },
  {
    id: 'T-38',
    title: 'Файловое хранилище: учения восстановления',
    status: 'archived',
    closed_reason: 'done',
    archivedAt: 8 * D,
    tags: ['topic:files', 'topic:backup', 'area:work'],
    places: [[INFRA, 'primary']],
    createdAt: 12 * D,
    updatedAt: 8 * D,
    journal: [{ kind: 'log', at: 8 * D, source: 'buildbox · runner', body: 'Восстановление из бэкапа за 14 минут, контрольные суммы совпали.' }],
  },
  {
    id: 'T-36',
    title: 'Try a hosted wiki as the task store',
    status: 'archived',
    closed_reason: 'cancelled',
    archivedAt: 3 * D,
    tags: ['topic:agent-tasks', 'area:personal'],
    createdAt: 7 * D,
    updatedAt: 3 * D,
    docs: [{ slug: 'research', kind: 'research', title: 'A hosted wiki as a task store', revisions: [{ body: wikiStoreResearch, author: 'agent', at: 6 * D }] }],
  },
  {
    id: 'T-35',
    title: 'Dev containers on rootless Docker',
    status: 'archived',
    closed_reason: 'done',
    archivedAt: 10 * D,
    tags: ['topic:docker', 'security'],
    places: [[INFRA, 'primary'], [DEVENV, 'touched', 'Image and entrypoint changes']],
    createdAt: 18 * D,
    updatedAt: 10 * D,
  },
  {
    id: 'T-47',
    title: 'Разобрать почту за неделю',
    status: 'active',
    tags: ['area:personal'],
    createdAt: 8,
    updatedAt: 8,
  },
];
