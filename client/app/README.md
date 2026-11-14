# @kit/app

React 19 + Vite + TypeScript client for the typing course.

## Development

From the repository root:

```bash
npm install
npm run dev:app
```

The app runs at http://localhost:5173. The identity service runs separately on http://localhost:3001 via `npm run dev:user`, after `npm run db:up` and `npm run migrate:up`. Account screens are not part of the app yet.

## Layout

- `src/pages/` - page-level orchestration
- `src/components/` - reusable interface elements
- `src/curriculum.ts` - chapters, levels, and authored lesson content
- `src/courseProgress.ts`, `src/progress.ts`, `src/dailyProgress.ts` - progress, scoring, and streaks in local storage
- `src/sound/` - Howler.js cues
- `src/assets/` - images and fonts

## Checks

```bash
npm run typecheck
npm run lint
npm run test:lesson
```

Course behavior is described in [`docs/systems/learning-path.md`](../../docs/systems/learning-path.md).
