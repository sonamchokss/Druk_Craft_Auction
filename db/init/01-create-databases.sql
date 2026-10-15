-- One database per service (plan section 2). Runs once, on first Postgres start.
-- To re-run: docker compose down -v
CREATE DATABASE listings_db;
CREATE DATABASE bids_db;
CREATE DATABASE timers_db;
CREATE DATABASE payments_db;
CREATE DATABASE notifications_db;
