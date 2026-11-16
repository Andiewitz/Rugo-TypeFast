# Learning path

The course is a single ordered path of 13 chapters, five levels each: left-hand letters, right-hand letters, both hands, capitalization, punctuation, sentences, numbers, paragraphs and whitespace, symbols, Java basics, Java decisions and arrays, Java classes and methods, and mixed mastery.

## Course structure

- The path shows all 65 levels as circular nodes. Horizontal positions vary between visits, and future levels stay locked.
- Locked labels use dark, readable text.
- Each new lesson opens with an introduction, a practice preview, and chapter-specific tips. Replays go straight to practice.
- Every level has 10 randomized rounds drawn from at least ten distinct, authored examples for that level. Hand drills add fresh key or word practice, and short complete examples may be paired into longer rounds without scrambling their words.
- The WPM goal and mistake allowance come from the chapter's difficulty and apply to each round independently.
- Finishing a round starts the next one. A failed round can be retried without losing the rounds already cleared.
- Clearing all ten rounds unlocks the next level and awards 20 XP on the first clear. Replays can improve the saved best stars but do not repeat the XP reward.

## Typing rules

- The first three chapters accept uppercase and lowercase as equivalent. From chapter four, case must match.
- In whitespace exercises Enter is displayed as `↵` and Tab as `⇥`. When Tab is the next expected character, the Tab key inserts it; Shift+Tab still moves backward. Keyboards without a Tab key can use the Insert Tab button.
- Mistakes must be corrected before advancing. Exceeding the round's allowance ends it, and finishing below the WPM goal also fails. Results explain the failure and offer a retry of that round.
- The final result aggregates the completed rounds from their total active time and character attempts; failed attempts that were retried are not included. The arrow at the top left returns to the learning path.
- WPM counts correct characters as five-character words over active typing time, so pausing does not lower it.

## Progress and storage

- Progress, XP, best WPM, activity dates, and daily goal totals live in the browser's local storage under `rugo-progress`. They belong to that browser and origin, are not synced to an account, and a notice appears on the main screen when storage is unavailable.
- Daily goals reset at the next local calendar day. The league page is informational until an online leaderboard exists.
- Six-level course progress is archived in `legacyCourse`. Upgrading from the previous three-level chapters keeps original completed levels, stars, unlocks, XP, and records; the newly added fourth and fifth levels in previously unlocked chapters are available but marked uncompleted.
- Original chapter-course results are archived in `previousCourse` and can be reviewed in Profile.

## Presentation

- Opening an exercise from the path or Practice shows a brief full-screen preparation screen, then continues automatically. Retrying from results starts a fresh attempt directly.
- Lessons use a focused layout; navigation and progress return when the learner goes back to the path.
- Rugo is a reusable SVG character with independently animated head, eyes, eyebrows, mouth, body, arms, legs, and tail. The path mascot cycles through idle, happy, curious, thinking, surprised, proud, and celebration expressions, walks side-on during preparation, and reacts to results with distinct celebration, surprise, thought, and encouragement shapes. Motion respects reduced-motion settings.
- On the learning path Rugo stays visible while scrolling; smaller screens use a compact corner placement with a smaller footprint.
- Compact screens adapt the learning path, progress panels, keyboard guide, and results to the available width. Lesson and results action bars stay pinned to the bottom of the viewport while content scrolls.
- The earlier Rive and illustrated experiments are retained as source material and are not used in the app.

## Sound

- Sound effects use Howler.js with short locally generated cues for starting a lesson, typing, mistakes, combo milestones, and results. Five, ten, twenty, thirty, fifty, seventy-five, and one hundred correct keys each trigger a short milestone, and larger milestones have a distinct cue.
- Rugo reacts to long combos and offers encouragement after a mistake.
- The Sound control is available on the learning path, during lessons, and on results; the setting persists on the device. Sounds start only after interaction and can be muted.

## Tests

Run `npm run test:lesson` to check the curriculum, case and whitespace handling, round generation and aggregation, migration, scoring, correction, stars, mistake summaries, and daily rollover rules.
