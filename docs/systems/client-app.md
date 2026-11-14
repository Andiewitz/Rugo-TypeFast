# Client app

- Location: `client/app`
- Package: `@kit/app`
- Runtime: browser
- Stack: React 19, Vite, TypeScript
- Responsibility: the typing course interface, local progress, and sound

## Rendering flow

`App` wraps `LearnPage` in `SoundProvider`. `LearnPage` owns all page state and renders one of the screens, with the sidebar, progress panel, and storage notice around it.

```
App
└── SoundProvider
    └── LearnPage
        ├── Sidebar            navigation, sound toggle, layout label
        ├── LearningPath       the 65-level path          (page = Learn)
        ├── PracticePage       warm-up exercise           (page = Practice)
        ├── SettingsPage       build version              (page = Settings)
        ├── InfoPage           Leagues, Quests, Profile   (other pages)
        └── ProgressPanel      XP, streak, daily goals
```

## Pages

| Page | Component | Contents |
| --- | --- | --- |
| `Learn` | `LearningPath` | Chapter and level nodes, unlock state, best stars |
| `Practice` | `PracticePage` | Home-row warm-up |
| `Leagues` | `InfoPage` | League placeholder until a leaderboard exists |
| `Quests` | `InfoPage` | Today's lessons, best speed, daily goals |
| `Profile` | `InfoPage` | Totals, best speed, archived course history |
| `Settings` | `SettingsPage` | Build version |

Leaving a lesson returns to the path. `window.scrollTo(0, 0)` runs on every navigation.

## State

`LearnPage` holds `page`, `lesson`, `loadingLesson`, and `progress`. Lessons run `startLesson` -> `LessonLoading` -> `Lesson` -> `finish`, and `finish` is the only writer of scored progress: it raises the completed count, keeps the best stars, awards the 20 XP first-clear bonus, updates best WPM, and records today's totals and streak date.

Progress is read once through `readProgress`, which migrates older stored shapes (`courseProgress.ts`) and sanitises star arrays before use.

## Storage

Everything persists in local storage under `rugo-progress`: completed levels, stars, XP, best WPM, activity dates, and daily totals. A failed write surfaces the "Progress could not be saved" notice. Daily totals reset when the calendar day changes, checked by a one-minute timer and on `visibilitychange`.

Storage is per browser and origin; no account sync exists yet.

## Lesson internals

`components/lesson` holds the typing engine: prompt rendering, per-character comparison, WPM and accuracy calculation, correction handling, star scoring, and the results screen. `curriculum.ts` supplies chapters, levels, and authored examples; `npm run test:lesson` covers those rules.

## Sound

`SoundProvider` exposes `enabled`, `toggle`, and `play`. `components/sound` maps cues to locally generated clips played through Howler.js. `SoundToggle` appears in the sidebar, in lessons, and on results.

## Build version

`vite.config.ts` reads the root `package.json` and defines `__APP_VERSION__`, which `SettingsPage` renders under **Settings → Build Version**.

## Checks

```bash
npm run typecheck
npm run lint
npm run test:lesson
```

Course behavior is documented in [`learning-path.md`](learning-path.md).
