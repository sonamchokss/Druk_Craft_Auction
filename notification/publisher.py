import json, sys, uuid
from datetime import datetime, timezone
import urllib.request

event_type = sys.argv[1] if len(sys.argv) > 1 else "BidPlaced"
payloads = {
 "BidPlaced": {"auctionId":"demo-1","itemTitle":"Dragon Mask","bidderId":"U-3","amount":4500,"previousBidderId":"U-7","previousAmount":4000},
 "AuctionExtended": {"auctionId":"demo-1","newEndsAt":"2026-10-20T12:00:00Z"},
 "AuctionClosed": {"auctionId":"demo-1","itemTitle":"Dragon Mask","artisanId":"A-1","winnerId":"U-3","finalPrice":4500},
 "PaymentCompleted": {"auctionId":"demo-1","winnerId":"U-3","artisanId":"A-1","amount":4500,"receiptNo":"RCP-DEMO-1"},
}
body={"eventId":str(uuid.uuid4()),"eventType":event_type,"occurredAt":datetime.now(timezone.utc).isoformat(),"source":"demo-publisher","data":payloads.get(event_type,payloads["BidPlaced"])}
print(json.dumps(body))
print("Publish this JSON to exchange auction.events with routing key:", event_type.replace("Auction","auction.").replace("Bid","bid.").replace("Payment","payment.").lower())
