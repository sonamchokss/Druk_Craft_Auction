#!/usr/bin/env python3
"""Integration tests T1-T11 (group plan section 10 / lab Part G).

Runs against the stack started with `docker compose up --build` and prints
the expected and actual result of each test as a Markdown table.

    python3 scripts/integration_test.py            # from the repo root
    python3 scripts/integration_test.py > results.md

Needs only Python 3 and docker compose. Takes about 4 minutes: real
auctions have to run out. It stops and starts the payment service (T6/T7).
"""
import base64
import json
import subprocess
import sys
import time
import urllib.error
import urllib.request
from datetime import datetime, timezone

LISTING, BIDDING, PAYMENT, NOTIFY = (f"http://localhost:{p}" for p in (8001, 8002, 8004, 8005))
MQ = "http://localhost:15672/api"
MQ_AUTH = "Basic " + base64.b64encode(b"guest:guest").decode()
CAPTURE = "itest.capture"  # throwaway queue bound to every event
RUN = datetime.now().strftime("%H%M%S")  # fresh buyer ids each run, so wallets start full
BUYER_A, BUYER_B, BUYER_C, ARTISAN = f"U-T{RUN}a", f"U-T{RUN}b", f"U-T{RUN}c", "A-1"
STARTED_AT = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")

results = []


def http(method, url, body=None, auth=None):
    data = None if body is None else (body if isinstance(body, bytes) else json.dumps(body).encode())
    req = urllib.request.Request(url, data=data, method=method, headers={"Content-Type": "application/json"})
    if auth:
        req.add_header("Authorization", auth)
    try:
        with urllib.request.urlopen(req, timeout=15) as resp:
            raw = resp.read()
            return resp.status, (json.loads(raw) if raw else None)
    except urllib.error.HTTPError as err:
        raw = err.read()
        try:
            return err.code, json.loads(raw)
        except ValueError:
            return err.code, raw.decode(errors="replace")


def mq(method, path, body=None):
    return http(method, MQ + path, body, MQ_AUTH)


def publish(routing_key, payload: str):
    status, body = mq("POST", "/exchanges/%2F/auction.events/publish", {
        "properties": {"delivery_mode": 2, "content_type": "application/json"},
        "routing_key": routing_key, "payload": payload, "payload_encoding": "string"})
    assert status == 200 and body.get("routed"), f"publish failed: {status} {body}"


captured = []  # every event seen on the capture queue, in order


def drain():
    """Move new messages from the capture queue into `captured`."""
    _, msgs = mq("POST", f"/queues/%2F/{CAPTURE}/get", {"count": 200, "ackmode": "ack_requeue_false", "encoding": "auto"})
    for m in msgs or []:
        try:
            captured.append((m["routing_key"], m["payload"], json.loads(m["payload"])))
        except ValueError:
            captured.append((m["routing_key"], m["payload"], None))


def events(event_type, auction_id=None):
    drain()
    return [(raw, env) for _, raw, env in captured
            if env and env.get("eventType") == event_type and (auction_id is None or env["data"].get("auctionId") == auction_id)]


def wait_for(check, timeout, every=1.0):
    end = time.time() + timeout
    while time.time() < end:
        value = check()
        if value:
            return value
        time.sleep(every)
    return check()


def queue_ready(name):
    _, q = mq("GET", f"/queues/%2F/{name}")
    return q.get("messages_ready", 0) if isinstance(q, dict) else 0


def logs(service):
    out = subprocess.run(["docker", "compose", "logs", "--no-log-prefix", "--since", STARTED_AT, service],
                         capture_output=True, text=True)
    return out.stdout


def logged(service, event_id, result):
    """True if the service logged this eventId with this result (both log formats)."""
    return any(event_id in line and (f"result={result}" in line or f'"result":"{result}"' in line)
               for line in logs(service).splitlines())


def listing(item_id):
    return http("GET", f"{LISTING}/listings/{item_id}")[1]


def create_and_start(title, category, seconds):
    _, item = http("POST", f"{LISTING}/listings", {"title": title, "artisanId": ARTISAN, "category": category,
                                                   "startPrice": 3000, "durationSeconds": seconds})
    status, started = http("POST", f"{LISTING}/listings/{item['id']}/start")
    assert status == 200, f"start failed: {status} {started}"
    return item["id"], started["auctionId"], started["endsAt"]


def bid(auction_id, bidder, amount):
    return http("POST", f"{BIDDING}/auctions/{auction_id}/bids", {"bidderId": bidder, "amount": amount})


def record(test, scenario, expected, actual, passed):
    results.append((test, scenario, expected, actual, passed))
    print(f"{'PASS' if passed else 'FAIL'} {test} {scenario}: {actual}", file=sys.stderr)


def notification_types(user, auction_id):
    """Notification types this user received for this auction only (not earlier runs)."""
    _, notes = http("GET", f"{NOTIFY}/notifications?userId={user}")
    return sorted({n["type"] for n in (notes or []) if n.get("auctionId") == auction_id})


def parse_time(value):
    return datetime.fromisoformat(value.replace("Z", "+00:00"))


def main():
    # Capture queue: a copy of every event on the exchange, for checking payloads.
    mq("PUT", f"/queues/%2F/{CAPTURE}", {"durable": False, "auto_delete": False})
    mq("POST", f"/bindings/%2F/e/auction.events/q/{CAPTURE}", {"routing_key": "#"})
    mq("DELETE", f"/queues/%2F/{CAPTURE}/contents")
    try:
        run_tests()
    finally:
        mq("DELETE", f"/queues/%2F/{CAPTURE}")
        subprocess.run(["docker", "compose", "start", "payment"], capture_output=True)
    print_table()
    return 0 if all(r[4] for r in results) else 1


def run_tests():
    # ---- Main workflow: list -> bid war -> late bid -> close -> pay ----------
    wallet_a = http("GET", f"{PAYMENT}/wallets/{BUYER_A}")[1]["balance"]
    item, auction, ends_at = create_and_start("Hand-painted Thangka - Green Tara", "Thangkas", 40)

    # T2 event delivery: AuctionStarted reaches Bidding and Timer.
    started = wait_for(lambda: events("AuctionStarted", auction), 10)
    started_id = started[0][1]["eventId"] if started else ""
    bidding_open = wait_for(lambda: http("GET", f"{BIDDING}/auctions/{auction}/bids")[0] == 200, 10)
    timer_ok = wait_for(lambda: logged("timer", started_id, "processed"), 10)
    record("T2", "Event delivery", "AuctionStarted processed by Timer (end time stored) and Bidding (auction opened)",
           f"Timer logged processed={bool(timer_ok)}; Bidding auction {auction} open={bool(bidding_open)}; endsAt {ends_at}",
           bool(started and timer_ok and bidding_open))

    # T1 successful operation: a valid bid is saved and BidPlaced is published.
    status, first = bid(auction, BUYER_A, 3500)
    placed = wait_for(lambda: events("BidPlaced", auction), 10)
    data = placed[0][1]["data"] if placed else {}
    record("T1", "Successful operation", "Bid saved (201), BidPlaced published",
           f"HTTP {status}, bid {first.get('bidId') if isinstance(first, dict) else first}; BidPlaced amount={data.get('amount')} previousBidderId={data.get('previousBidderId')}",
           status == 201 and data.get("amount") == 3500 and data.get("previousBidderId") is None)

    # T10 invalid bid: rejected with a message, and no event published.
    before = len(events("BidPlaced", auction))
    status, err = bid(auction, BUYER_B, 3200)
    time.sleep(2)
    after = len(events("BidPlaced", auction))
    record("T10", "Invalid bid feedback", "Rejected with an error message; no event published",
           f"HTTP {status} \"{err.get('error') if isinstance(err, dict) else err}\"; BidPlaced events {before} -> {after}",
           status == 422 and "too low" in str(err) and before == after)

    bid(auction, BUYER_B, 4000)  # outbids A

    # T5 anti-sniping: a bid in the last 30 s extends the auction by 30 s.
    # Compare with the endsAt Timer received in AuctionStarted (whole seconds);
    # Listing's REST response carries the same time with sub-second precision.
    deadline = parse_time(started[0][1]["data"]["endsAt"])
    time.sleep(max(0, (deadline - datetime.now(timezone.utc)).total_seconds() - 20))
    status, _ = bid(auction, BUYER_A, 4500)
    extended = wait_for(lambda: events("AuctionExtended", auction), 10)
    new_end = parse_time(extended[0][1]["data"]["newEndsAt"]) if extended else None
    record("T5", "Anti-sniping chain", "BidPlaced in the last 30 s -> AuctionExtended, end time +30 s",
           f"late bid HTTP {status}; AuctionExtended newEndsAt = original + {(new_end - deadline).total_seconds():.0f} s" if new_end else "no AuctionExtended",
           bool(new_end) and round((new_end - deadline).total_seconds()) == 30)

    # T3 multiple consumers / T4 chaining: the close is processed by 4 services and leads to Sold.
    closed = wait_for(lambda: events("AuctionClosed", auction), 90)
    closed_id = closed[0][1]["eventId"] if closed else ""
    consumers = {s: bool(wait_for(lambda s=s: logged(s, closed_id, "processed"), 20)) for s in ("payment", "listing", "bidding", "notification")}
    record("T3", "Multiple consumers", "AuctionClosed processed by Payment, Listing, Bidding and Notification",
           f"AuctionClosed winner={closed[0][1]['data'].get('winnerId') if closed else None}; processed by " + ", ".join(f"{s}={ok}" for s, ok in consumers.items()),
           bool(closed) and all(consumers.values()))

    sold = wait_for(lambda: listing(item)["status"] == "Sold", 30)
    completed = events("PaymentCompleted", auction)
    record("T4", "Event chaining", "AuctionClosed -> PaymentCompleted -> item marked Sold",
           f"PaymentCompleted receipt={completed[0][1]['data'].get('receiptNo') if completed else None}; listing status={listing(item)['status']}",
           bool(sold and completed))

    # T11 complete workflow: receipt, wallet, every notification, closed auction.
    _, payments = http("GET", f"{PAYMENT}/payments?buyerId={BUYER_A}")
    wallet_after = http("GET", f"{PAYMENT}/wallets/{BUYER_A}")[1]["balance"]
    late_status, _ = bid(auction, BUYER_B, 9999)
    notes = {u: notification_types(u, auction) for u in (BUYER_A, BUYER_B)}
    want_a, want_b = {"outbid", "extended", "won", "receipt"}, {"outbid", "extended"}
    artisan_notes = notification_types(ARTISAN, auction)
    record("T11", "Complete workflow", "Receipt issued, wallet charged once, item Sold, all notifications delivered, closed auction rejects bids",
           f"{len(payments)} receipt(s); wallet {wallet_a} -> {wallet_after}; winner notified {notes[BUYER_A]}; outbid bidder {notes[BUYER_B]}; artisan has sold={'sold' in artisan_notes}; bid after close HTTP {late_status}",
           len(payments) == 1 and wallet_a - wallet_after == 4500 and want_a <= set(notes[BUYER_A])
           and want_b <= set(notes[BUYER_B]) and "sold" in artisan_notes and late_status == 409)

    # ---- Failure and recovery (plan section 6.1) -----------------------------
    subprocess.run(["docker", "compose", "stop", "payment"], capture_output=True, check=True)
    item2, auction2, _ = create_and_start("Yathra Kira, Indigo", "Textiles", 45)
    time.sleep(1.5)
    bid(auction2, BUYER_C, 3500)  # 43 s left: outside the anti-sniping window
    closed2 = wait_for(lambda: events("AuctionClosed", auction2), 90)
    waiting = wait_for(lambda: queue_ready("payment.queue") == 1, 20)
    record("T6", "Consumer unavailable", "Payment stopped; message waits in payment.queue; rest of system works",
           f"payment stopped; AuctionClosed published={bool(closed2)}; payment.queue ready={queue_ready('payment.queue')}; listing status={listing(item2)['status']}",
           bool(closed2 and waiting) and listing(item2)["status"] == "Awaiting Payment")

    subprocess.run(["docker", "compose", "start", "payment"], capture_output=True, check=True)
    sold2 = wait_for(lambda: listing(item2)["status"] == "Sold", 60)
    drained = wait_for(lambda: queue_ready("payment.queue") == 0, 20)
    _, payments_c = http("GET", f"{PAYMENT}/payments?buyerId={BUYER_C}")
    wallet_c = http("GET", f"{PAYMENT}/wallets/{BUYER_C}")[1]["balance"]
    record("T7", "Consumer recovery", "Queued message processed after restart, receipt issued",
           f"payment started; listing status={listing(item2)['status']}; payment.queue ready={queue_ready('payment.queue')}; receipts={[p['receiptNo'] for p in payments_c]}",
           bool(sold2 and drained) and len(payments_c) == 1)

    # T8 duplicate: re-publish the exact captured AuctionClosed (same eventId).
    raw, env = closed2[0]
    publish("auction.closed", raw)
    dup = wait_for(lambda: logged("payment", env["eventId"], "duplicate-skipped"), 20)
    time.sleep(2)
    _, payments_c2 = http("GET", f"{PAYMENT}/payments?buyerId={BUYER_C}")
    wallet_c2 = http("GET", f"{PAYMENT}/wallets/{BUYER_C}")[1]["balance"]
    record("T8", "Duplicate event", "Logged as duplicate; no second invoice or charge",
           f"Payment logged duplicate-skipped={bool(dup)}; invoices {len(payments_c)} -> {len(payments_c2)}; wallet {wallet_c} -> {wallet_c2}",
           bool(dup) and len(payments_c2) == len(payments_c) and wallet_c2 == wallet_c)

    # T9 processing error: a malformed message is retried 3 times, then dead-lettered.
    dlq_before = queue_ready("payment.dlq")
    publish("auction.closed", "this is not a valid event")
    in_dlq = wait_for(lambda: queue_ready("payment.dlq") == dlq_before + 1, 30)
    retries = sum(1 for line in logs("payment").splitlines() if '"result":"retry"' in line and "invalid envelope" in line)
    record("T9", "Processing error", "Retried 3 times, then in payment.dlq",
           f"payment logged {retries} retries; payment.dlq {dlq_before} -> {queue_ready('payment.dlq')}",
           bool(in_dlq) and retries >= 3)


def print_table():
    order = ["T1", "T2", "T3", "T4", "T5", "T6", "T7", "T8", "T9", "T10", "T11"]
    print(f"Run at {STARTED_AT} (buyers {BUYER_A}, {BUYER_B}, {BUYER_C})\n")
    print("| # | Scenario | Expected result | Actual result | Pass? |")
    print("|---|---|---|---|---|")
    for test, scenario, expected, actual, passed in sorted(results, key=lambda r: order.index(r[0])):
        print(f"| {test} | {scenario} | {expected} | {actual} | {'✅ Pass' if passed else '❌ Fail'} |")


if __name__ == "__main__":
    sys.exit(main())
