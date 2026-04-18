package db

import (
	"database/sql"
	"fmt"

	"github.com/forgemill/forgemill/internal/db/models"
)

// CreateNotification inserts a new notification. UserID nil means broadcast.
func (db *DB) CreateNotification(n *models.Notification) error {
	var userID interface{}
	if n.UserID != nil {
		userID = *n.UserID
	}
	res, err := db.conn.Exec(
		`INSERT INTO notifications (user_id, level, title, body, link, event, is_read)
		 VALUES (?, ?, ?, ?, ?, ?, FALSE)`,
		userID, n.Level, n.Title, n.Body, n.Link, n.Event,
	)
	if err != nil {
		return fmt.Errorf("insert notification: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("notification id: %w", err)
	}
	n.ID = id
	return nil
}

// ListNotificationsForUser returns a user's notifications plus broadcasts
// (user_id IS NULL). Ordered newest first, limited to N rows.
// If unreadOnly is true, only unread rows are returned.
func (db *DB) ListNotificationsForUser(userID int64, unreadOnly bool, limit int) ([]models.Notification, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := `SELECT id, user_id, level, title, body, link, event, is_read, created_at, read_at
	          FROM notifications
	          WHERE (user_id = ? OR user_id IS NULL)`
	if unreadOnly {
		query += ` AND is_read = FALSE`
	}
	query += ` ORDER BY created_at DESC LIMIT ?`

	rows, err := db.conn.Query(query, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("query notifications: %w", err)
	}
	defer rows.Close()

	out := []models.Notification{}
	for rows.Next() {
		var n models.Notification
		var uid sql.NullInt64
		if err := rows.Scan(&n.ID, &uid, &n.Level, &n.Title, &n.Body, &n.Link, &n.Event, &n.IsRead, &n.CreatedAt, &n.ReadAt); err != nil {
			return nil, fmt.Errorf("scan notification: %w", err)
		}
		if uid.Valid {
			v := uid.Int64
			n.UserID = &v
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// CountUnreadForUser returns the number of unread notifications a user has.
// Broadcasts are counted unread if not in notification_reads (for now we
// keep it simple and broadcasts have no per-user read state).
func (db *DB) CountUnreadForUser(userID int64) (int, error) {
	var count int
	err := db.conn.QueryRow(
		`SELECT COUNT(*) FROM notifications
		 WHERE (user_id = ? OR user_id IS NULL) AND is_read = FALSE`,
		userID,
	).Scan(&count)
	return count, err
}

// MarkNotificationRead marks a single notification read.
// Only the target user (or recipient of a broadcast) may mark it read.
func (db *DB) MarkNotificationRead(id, userID int64) error {
	res, err := db.conn.Exec(
		`UPDATE notifications SET is_read = TRUE, read_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND (user_id = ? OR user_id IS NULL) AND is_read = FALSE`,
		id, userID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("notification not found or already read")
	}
	return nil
}

// MarkAllNotificationsRead marks all of the user's unread notifications read.
func (db *DB) MarkAllNotificationsRead(userID int64) (int64, error) {
	res, err := db.conn.Exec(
		`UPDATE notifications SET is_read = TRUE, read_at = CURRENT_TIMESTAMP
		 WHERE (user_id = ? OR user_id IS NULL) AND is_read = FALSE`,
		userID,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeleteNotification removes a single notification owned by the user.
func (db *DB) DeleteNotification(id, userID int64) error {
	res, err := db.conn.Exec(
		`DELETE FROM notifications WHERE id = ? AND (user_id = ? OR user_id IS NULL)`,
		id, userID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("notification not found")
	}
	return nil
}

// DeleteOldReadNotifications removes read notifications older than olderThanDays.
// Broadcast notifications (user_id IS NULL) with no readers are also expired
// after that window to keep the table bounded.
func (db *DB) DeleteOldReadNotifications(olderThanDays int) (int64, error) {
	if olderThanDays <= 0 {
		olderThanDays = 30
	}
	res, err := db.conn.Exec(
		fmt.Sprintf(`DELETE FROM notifications
		 WHERE (is_read = TRUE AND read_at < datetime('now', '-%d days'))
		    OR created_at < datetime('now', '-%d days')`, olderThanDays, olderThanDays*3),
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ListAdminUserIDs returns the IDs of all active admin users. Used for
// broadcasting admin-only notifications.
func (db *DB) ListAdminUserIDs() ([]int64, error) {
	rows, err := db.conn.Query(`SELECT id FROM users WHERE role = 'admin' AND is_active = TRUE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
