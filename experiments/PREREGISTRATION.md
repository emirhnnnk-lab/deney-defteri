# Pre-registration: ab-ticket-routing-3b-vs-7b

Written 2026-11-10, before the first run.

**Question.** For routing short support tickets (5 classes, Turkish + English), is the bigger local model worth the extra latency?

**Setup.** 20 hand-labelled tickets, 4 per class. `qwen2.5:3b` vs `qwen2.5:7b` on Ollama, same system prompt, calls interleaved in a fixed random order (seed 42). Calls go through the free-model bridge (`kopru._sor`) and are logged by the bridge's own `_log`.

**Prediction.**
1. 7b is at least 10 points more accurate than 3b.
2. 3b is at least 1.5x faster per call (mean seconds).

**Decision rule.** If 3b is within 5 points of 7b, the bridge's default for classification jobs moves to 3b. Otherwise it stays on 7b.

**Run.** `--rounds 3`: 60 calls per arm, but only 20 distinct tickets, so repeats are not independent samples.

**Known limits.** 20 distinct tickets per arm is small; a 10-point gap is 2 tickets. The result is a direction, not proof. The first call of each model includes model load time.
