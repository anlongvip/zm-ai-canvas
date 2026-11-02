-- Run with psql -X -v ON_ERROR_STOP=1 -v channel_id=<verified-channel-id>.
-- Reports persisted evidence only; never calls an upstream or changes funds.
\set ON_ERROR_STOP on
\pset pager off
BEGIN READ ONLY;

SELECT b.status AS billing_status,
       COALESCE(t.status, 'no_task') AS task_status,
       count(*) AS orders,
       sum(b.actual_amount_microcredits) AS actual_microcredits,
       sum(b.refunded_amount_microcredits) AS refunded_microcredits
FROM billing_orders b
LEFT JOIN tasks t ON t.billing_order_id = b.id
WHERE b.channel_id = :'channel_id'
GROUP BY b.status, t.status
ORDER BY b.status, t.status;

WITH ledger AS (
  SELECT b.id, b.status, b.actual_amount_microcredits,
         b.refunded_amount_microcredits,
         count(l.id) AS ledger_entries,
         count(l.id) FILTER (WHERE l.type = 'refund') AS refunds,
         COALESCE(sum(l.available_delta_microcredits), 0) AS available_net,
         COALESCE(sum(l.reserved_delta_microcredits), 0) AS reserved_net,
         COALESCE(sum(l.available_delta_microcredits)
           FILTER (WHERE l.type = 'refund'), 0) AS returned
  FROM billing_orders b
  LEFT JOIN credit_ledger_entries l ON l.billing_order_id = b.id
  WHERE b.channel_id = :'channel_id' AND b.status IN ('settled', 'refunded')
  GROUP BY b.id
)
SELECT status, count(*) AS terminal_orders,
       count(*) FILTER (WHERE ledger_entries > 0 AND reserved_net = 0 AND
         CASE WHEN status = 'refunded' THEN
           actual_amount_microcredits = 0 AND available_net = 0 AND
           refunds = 1 AND returned = refunded_amount_microcredits
         ELSE available_net = -actual_amount_microcredits END
       ) AS consistent_orders
FROM ledger
GROUP BY status
ORDER BY status;

SELECT count(*) FILTER (WHERE t.status IN ('failed', 'cancelled') AND
         b.status = 'refunded') AS failed_or_cancelled_refunded_samples,
       count(*) FILTER (WHERE t.status IN ('failed', 'cancelled') AND
         b.status IN ('reserved', 'running', 'uncertain')) AS unresolved_samples,
       count(*) FILTER (WHERE t.status IN ('failed', 'cancelled') AND
         b.status = 'refunded' AND NULLIF(t.provider_request_id, '') IS NOT NULL
       ) AS refunded_samples_with_provider_id
FROM billing_orders b
JOIN tasks t ON t.billing_order_id = b.id
WHERE b.channel_id = :'channel_id';

COMMIT;
