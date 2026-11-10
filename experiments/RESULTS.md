# Results: ab-ticket-routing-3b-vs-7b (run 2026-11-10)

Raw output: `run_2026-11-10.txt`. Rows: the bridge's `kullanim_log.jsonl` (`"deney": "ab-ticket-routing-3b-vs-7b"`).
Hardware: NVIDIA GTX 1650, 4 GB VRAM, Ollama.

| | qwen2.5:3b | qwen2.5:7b |
|---|---|---|
| Calls | 60 | 60 |
| Correct | 42 (70%) | 51 (85%) |
| Median latency | 0.78 s | 2.44 s |
| Mean latency | 4.27 s | 8.62 s |
| Calls over 10 s | 3 | 27 |

**Prediction 1 (7b ≥ 10 points more accurate): confirmed**, +15 points. The two-proportion test gives p ≈ 0.049, but the 60 calls per arm are 20 tickets repeated 3 times, so the real evidence is weaker than that number suggests.

**Prediction 2 (3b ≥ 1.5x faster): confirmed on the median (3.1x), confounded on the mean.** I interleaved the calls to avoid order bias. On a 4 GB GPU, that made Ollama unload one model and load the other again and again. 27 of the 7b calls took more than 10 s. Without those calls, the 7b mean is 2.01 s. The comparison of means is not clean.

**Errors.** Both models mostly sent account tickets (password reset, 2FA) to `technical`: 8 times for 3b, 6 times for 7b. 7b sent 3 billing tickets to `refund`. 3b returned the malformed label `shippinging` 3 times.

**Decision.** 3b is not within 5 points of 7b, so classification jobs in the bridge stay on 7b (pre-registered rule).

**Next run.** Run each model as a block with one warm-up call, and add one-line label definitions to the prompt (account vs technical).
