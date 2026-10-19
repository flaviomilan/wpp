package archive

import (
	"database/sql"
	"encoding/json"
	"errors"
)

func legacyReaction(m Message) bool {
	return !m.Deleted && !m.Edited && !m.Mutation && m.Text == "" &&
		(m.Kind == "não interpretado (reactionMessage)" || m.Kind == "não interpretado (encReactionMessage)")
}

// Older versions discarded the reaction payload. Identify the event, but do
// not invent its emoji or target. A later history replay can restore both.
func (s *Store) repairLegacyReactions() error {
	return s.transaction(func(tx *sql.Tx) error {
		rows, err := tx.Query("SELECT data FROM messages WHERE json_extract(data,'$.Kind') IN ('não interpretado (reactionMessage)','não interpretado (encReactionMessage)')")
		if err != nil {
			return err
		}
		defer rows.Close() //nolint:errcheck // Explicit closure or rows.Err below reports the error.
		var repairs []Message
		for rows.Next() {
			var raw []byte
			var m Message
			if err = rows.Scan(&raw); err != nil {
				closeErr := rows.Close()
				return errors.Join(err, closeErr)
			}
			if err = json.Unmarshal(raw, &m); err != nil {
				closeErr := rows.Close()
				return errors.Join(err, closeErr)
			}
			if legacyReaction(m) {
				m.Kind, m.Text = "reação", "Reação indisponível"
				repairs = append(repairs, m)
			}
		}
		err = errors.Join(rows.Err(), rows.Close())
		if err != nil {
			return err
		}
		for _, m := range repairs {
			if _, err = putMessage(tx, m); err != nil {
				return err
			}
		}
		return nil
	})
}
