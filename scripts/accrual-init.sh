#!/bin/sh
set -e

ACCRUAL_URL="${ACCRUAL_URL:-http://accrual:8080}"

echo "Waiting for accrual service at $ACCRUAL_URL..."
for i in $(seq 1 30); do
    if wget -q -O /dev/null "$ACCRUAL_URL/api/orders/0" 2>/dev/null; then
        echo "Accrual service is ready"
        break
    fi
    echo "Attempt $i/30: accrual not ready, waiting..."
    sleep 2
done

echo "Registering test reward mechanics..."

# Reward: 10% for items containing "Bork"
wget -q -O - --header="Content-Type: application/json" \
    --post-data='{"match":"Bork","reward":10,"reward_type":"%"}' \
    "$ACCRUAL_URL/api/goods" && echo "OK" || true

# Reward: 5% for items containing "Samsung"
wget -q -O - --header="Content-Type: application/json" \
    --post-data='{"match":"Samsung","reward":5,"reward_type":"%"}' \
    "$ACCRUAL_URL/api/goods" && echo "OK" || true

# Reward: 42 points for items containing "iPhone"
wget -q -O - --header="Content-Type: application/json" \
    --post-data='{"match":"iPhone","reward":42,"reward_type":"pt"}' \
    "$ACCRUAL_URL/api/goods" && echo "OK" || true

# Reward: 3% for items containing "LG"
wget -q -O - --header="Content-Type: application/json" \
    --post-data='{"match":"LG","reward":3,"reward_type":"%"}' \
    "$ACCRUAL_URL/api/goods" && echo "OK" || true

echo "Registering test orders..."

# Order with Bork item (10% of 7000 = 700)
wget -q -O - --header="Content-Type: application/json" \
    --post-data='{"order":"9278923470","goods":[{"description":"Чайник Bork","price":7000}]}' \
    "$ACCRUAL_URL/api/orders" && echo "OK" || true

# Order with Samsung item (5% of 15000 = 750)
wget -q -O - --header="Content-Type: application/json" \
    --post-data='{"order":"12345678903","goods":[{"description":"Телевизор Samsung","price":15000}]}' \
    "$ACCRUAL_URL/api/orders" && echo "OK" || true

# Order with iPhone item + LG item (42pt + 3% of 30000 = 942)
wget -q -O - --header="Content-Type: application/json" \
    --post-data='{"order":"346436439","goods":[{"description":"iPhone 15","price":50000},{"description":"Монитор LG","price":30000}]}' \
    "$ACCRUAL_URL/api/orders" && echo "OK" || true

# Order with no matching rewards
wget -q -O - --header="Content-Type: application/json" \
    --post-data='{"order":"79927398713","goods":[{"description":"Карандаш","price":100}]}' \
    "$ACCRUAL_URL/api/orders" && echo "OK" || true

echo "Accrual test data initialized successfully"
