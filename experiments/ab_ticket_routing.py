"""A/B experiment: qwen2.5:3b vs qwen2.5:7b on support-ticket routing.

Pre-registered prediction (written before the first run, see experiments/PREREGISTRATION.md):
  - 7b is at least 10 points more accurate than 3b
  - 3b is at least 1.5x faster (mean seconds per call)

Every call goes through the free-model bridge's own request and logging code
(araclar/belas-kopru/kopru.py: _sor, _log), so the rows land in the same
kullanim_log.jsonl the bridge writes in daily use. Two optional fields are added:
  "deney"  experiment id
  "dogru"  whether the predicted label matched the gold label

Run:  python experiments/ab_ticket_routing.py  [--rounds 1]
"""
import argparse
import random
import sys
import time
from pathlib import Path

KOPRU_DIR = Path(__file__).resolve().parents[3] / "araclar" / "belas-kopru"
sys.path.insert(0, str(KOPRU_DIR))
import kopru  # noqa: E402

EXPERIMENT_ID = "ab-ticket-routing-3b-vs-7b"
LABELS = ["billing", "technical", "account", "shipping", "refund"]
SYSTEM = (
    "You route customer support tickets. Answer with exactly one word from this list: "
    + ", ".join(LABELS)
    + ". No punctuation, no explanation."
)

# 20 short tickets with gold labels, 4 per class, mixed Turkish/English.
TICKETS = [
    ("I was charged twice for my subscription this month.", "billing"),
    ("Faturamda tanımadığım bir ek ücret var.", "billing"),
    ("Can I get an invoice with my company tax number on it?", "billing"),
    ("Kredi kartımdan neden iki kez çekim yapıldı?", "billing"),
    ("The app crashes every time I open the camera screen.", "technical"),
    ("Sayfa yüklenmiyor, 500 hatası alıyorum.", "technical"),
    ("Export to PDF produces a blank file.", "technical"),
    ("Bildirimler telefonuma gelmiyor, uygulama güncel.", "technical"),
    ("I forgot my password and the reset email never arrives.", "account"),
    ("Hesabımın e-posta adresini değiştirmek istiyorum.", "account"),
    ("Please delete my account and all my data.", "account"),
    ("İki adımlı doğrulamayı kapatamıyorum, giriş yapamıyorum.", "account"),
    ("My order says delivered but nothing arrived.", "shipping"),
    ("Kargom 6 gündür aynı şubede bekliyor.", "shipping"),
    ("Can you ship to Northern Cyprus?", "shipping"),
    ("Siparişimin teslimat adresini değiştirebilir miyim?", "shipping"),
    ("The jacket doesn't fit, I want my money back.", "refund"),
    ("Ürün kırık geldi, iade edip paramı almak istiyorum.", "refund"),
    ("I cancelled within 14 days, when will the refund hit my card?", "refund"),
    ("Yanlış ürün gönderildi, ücret iadesi istiyorum.", "refund"),
]


def normalize(text: str) -> str:
    word = text.strip().lower().split()[0] if text.strip() else ""
    return word.strip(".,:;!\"'`*")


def run(model: str, ticket: str, gold: str) -> None:
    kopru.AYAR["saglayicilar"]["ollama"]["model"] = model
    start = time.time()
    try:
        answer, usage = kopru._sor("ollama", ticket, SYSTEM, {})
    except Exception as e:  # logged as a failed call, same as the bridge does
        kopru._log({"gorev": "ab-siniflama", "saglayici": "ollama", "model": model, "basarili": False,
                    "hata": type(e).__name__, "deney": EXPERIMENT_ID})
        return
    seconds = round(time.time() - start, 2)
    pred = normalize(answer)
    kopru._log({"gorev": "ab-siniflama", "saglayici": "ollama", "model": model, "basarili": True,
                "sure_sn": seconds, "girdi_karakter": len(ticket), "cikti_karakter": len(answer),
                "kullanim": usage, "deney": EXPERIMENT_ID, "dogru": pred == gold})
    print(f"{model:12} {seconds:6.2f}s gold={gold:9} pred={pred:12} {'OK' if pred == gold else 'MISS'}")


def main() -> None:
    p = argparse.ArgumentParser()
    p.add_argument("--rounds", type=int, default=1)
    p.add_argument("--models", default="qwen2.5:3b,qwen2.5:7b")
    args = p.parse_args()
    models = args.models.split(",")
    rng = random.Random(42)
    for _ in range(args.rounds):
        jobs = [(m, t, g) for m in models for t, g in TICKETS]
        rng.shuffle(jobs)  # interleave models so warm-up and load don't favour one side
        for m, t, g in jobs:
            run(m, t, g)


if __name__ == "__main__":
    main()
