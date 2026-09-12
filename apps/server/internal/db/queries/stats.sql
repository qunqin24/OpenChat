-- name: GetDashboardTotals :one
SELECT (SELECT count(*) FROM users)                                  AS total_users,
       (SELECT count(*) FROM users WHERE disabled_at IS NULL)        AS active_users,
       (SELECT count(*) FROM conversations WHERE deleted_at IS NULL) AS total_conversations,
       (SELECT count(*) FROM messages)                               AS total_messages,
       (SELECT count(*) FROM users WHERE created_at >= now() - interval '7 days')         AS new_users_7d,
       (SELECT count(*) FROM messages WHERE created_at >= now() - interval '7 days')      AS messages_7d;

-- name: GetActivityTrend :many
-- One row per day for the requested window, including days with no activity —
-- a gap in the series would otherwise draw as a straight line across the chart.
SELECT d::date                                          AS day,
       coalesce(m.messages, 0)::bigint                  AS messages,
       coalesce(c.conversations, 0)::bigint             AS conversations,
       coalesce(u.users, 0)::bigint                     AS users
FROM generate_series(
             (current_date - (sqlc.arg('days')::int - 1)),
             current_date,
             interval '1 day'
     ) AS d
         LEFT JOIN (SELECT created_at::date AS day, count(*) AS messages
                    FROM messages
                    GROUP BY 1) m ON m.day = d::date
         LEFT JOIN (SELECT created_at::date AS day, count(*) AS conversations
                    FROM conversations
                    WHERE deleted_at IS NULL
                    GROUP BY 1) c ON c.day = d::date
         LEFT JOIN (SELECT created_at::date AS day, count(*) AS users
                    FROM users
                    GROUP BY 1) u ON u.day = d::date
ORDER BY d;
