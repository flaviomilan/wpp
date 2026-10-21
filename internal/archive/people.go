package archive

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Person struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	DetectedName string `json:"detected_name,omitempty"`
}
type PeopleFile struct {
	SchemaVersion int      `json:"schema_version"`
	GroupID       string   `json:"group_id"`
	People        []Person `json:"people"`
}

func migratePeople(tx *sql.Tx) error {
	for _, statement := range []string{
		"CREATE TABLE IF NOT EXISTS people_names(id TEXT PRIMARY KEY,name TEXT NOT NULL)",
		"CREATE TABLE IF NOT EXISTS group_people(chat TEXT NOT NULL REFERENCES chats(id),person TEXT NOT NULL,detected_name TEXT NOT NULL DEFAULT '',PRIMARY KEY(chat,person))",
	} {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	rows, err := tx.Query("SELECT data FROM messages WHERE chat LIKE '%@g.us'")
	if err != nil {
		return err
	}
	defer rows.Close() //nolint:errcheck // Explicit closure or rows.Err below reports the error.
	var messages []Message
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
		messages = append(messages, m)
	}
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil {
		return err
	}
	for _, m := range messages {
		if err = observePerson(tx, m.Chat, m.Sender, m.Author); err != nil {
			return err
		}
		if err = observePerson(tx, m.Chat, m.ReplySender, ""); err != nil {
			return err
		}
	}
	_, err = tx.Exec("PRAGMA user_version=3")
	return err
}

func validPerson(id string) bool {
	if strings.ContainsAny(id, "/\\:\x00 \t\n\r") {
		return false
	}
	user, server, ok := strings.Cut(id, "@")
	return ok && user != "" && (server == "s.whatsapp.net" || server == "lid")
}
func observePerson(tx *sql.Tx, group, id, name string) error {
	if !strings.HasSuffix(group, "@g.us") || !validPerson(id) {
		return nil
	}
	id, err := canonical(tx, id)
	if err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO group_people(chat,person,detected_name) VALUES(?,?,?) ON CONFLICT(chat,person) DO UPDATE SET detected_name=CASE WHEN excluded.detected_name<>'' THEN excluded.detected_name ELSE group_people.detected_name END", group, id, strings.TrimSpace(name))
	return err
}
func (s *Store) ObservePerson(group, id, name string) error {
	return s.transaction(func(tx *sql.Tx) error {
		if err := ensureChat(tx, group, ""); err != nil {
			return err
		}
		return observePerson(tx, group, id, name)
	})
}

func (s *Store) People(group string) (PeopleFile, error) {
	out := PeopleFile{SchemaVersion: 1, GroupID: group, People: []Person{}}
	if err := validateGroup(group); err != nil {
		return out, err
	}
	err := s.transaction(func(tx *sql.Tx) error {
		var exists int
		if err := tx.QueryRow("SELECT 1 FROM chats WHERE id=?", group).Scan(&exists); err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			return invalidInput("archive.group_not_found")
		}
		rows, err := tx.Query("SELECT gp.person,COALESCE(p.name,''),COALESCE(NULLIF(c.name,''),gp.detected_name) FROM group_people gp LEFT JOIN people_names p ON p.id=gp.person LEFT JOIN chats c ON c.id=gp.person WHERE gp.chat=? ORDER BY gp.person", group)
		if err != nil {
			return err
		}
		// Rows.Err reports iteration and automatic-close failures.
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var p Person
			if err = rows.Scan(&p.ID, &p.Name, &p.DetectedName); err != nil {
				return err
			}
			out.People = append(out.People, p)
		}
		return rows.Err()
	})
	return out, err
}
func (s *Store) RegisterPeople(file PeopleFile) (int, error) {
	if err := validateGroup(file.GroupID); err != nil {
		return 0, err
	}
	count := 0
	err := s.transaction(func(tx *sql.Tx) error {
		var exists int
		if err := tx.QueryRow("SELECT 1 FROM chats WHERE id=?", file.GroupID).Scan(&exists); err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			return invalidInput("archive.group_not_found")
		}
		seen := map[string]string{}
		for _, p := range file.People {
			if !validPerson(p.ID) {
				return invalidInput("archive.registration_contains_an_invalid_person_id_use_the_full_exported")
			}
			id, err := canonical(tx, p.ID)
			if err != nil {
				return err
			}
			name := strings.TrimSpace(p.Name)
			if name == "" {
				continue
			}
			if !utf8.ValidString(name) || utf8.RuneCountInString(name) > 128 {
				return invalidInput("archive.name_must_be_valid_utf_8_with_up_to_128")
			}
			for _, r := range name {
				if unicode.IsControl(r) {
					return invalidInput("archive.name_cannot_contain_control_characters")
				}
			}
			if old, exists := seen[id]; exists {
				if old != name {
					return invalidInput("archive.duplicate_person_names")
				}
				continue
			}
			seen[id] = name
			if err = observePerson(tx, file.GroupID, id, p.DetectedName); err != nil {
				return err
			}
			var old string
			err = tx.QueryRow("SELECT name FROM people_names WHERE id=?", id).Scan(&old)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if old == name {
				continue
			}
			if _, err = tx.Exec("INSERT INTO people_names(id,name) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name", id, name); err != nil {
				return err
			}
			count++
		}
		if count > 0 {
			_, err := tx.Exec("UPDATE chats SET generation=generation+1")
			return err
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

// Names are display metadata. They never modify original messages or revisions.
func (s *Store) Names() (map[string]string, error) {
	var out map[string]string
	err := s.transaction(func(tx *sql.Tx) error { var err error; out, err = namesTx(tx); return err })
	return out, err
}
func namesTx(tx *sql.Tx) (map[string]string, error) { return namesContextTx(context.Background(), tx) }
func namesContextTx(ctx context.Context, tx *sql.Tx) (map[string]string, error) {
	out := map[string]string{}
	err := (func() error {
		detected, err := tx.QueryContext(ctx, "SELECT person,MAX(detected_name) FROM group_people WHERE detected_name<>'' GROUP BY person")
		if err != nil {
			return err
		}
		defer func() { _ = detected.Close() }() // Explicit close before the next query.
		for detected.Next() {
			var id, name string
			if err = detected.Scan(&id, &name); err != nil {
				closeErr := detected.Close()
				return errors.Join(err, closeErr)
			}
			out[id] = name
		}
		err = errors.Join(detected.Err(), detected.Close())
		if err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, "SELECT id,name FROM chats WHERE name<>''")
		if err != nil {
			return err
		}
		defer rows.Close() //nolint:errcheck // Explicit closure or rows.Err below reports the error.
		for rows.Next() {
			var id, name string
			if err = rows.Scan(&id, &name); err != nil {
				closeErr := rows.Close()
				return errors.Join(err, closeErr)
			}
			out[id] = name
		}
		err = errors.Join(rows.Err(), rows.Close())
		if err != nil {
			return err
		}
		rows, err = tx.QueryContext(ctx, "SELECT id,name FROM people_names")
		if err != nil {
			return err
		}
		defer rows.Close() //nolint:errcheck // Explicit closure or rows.Err below reports the error.
		for rows.Next() {
			var id, name string
			if err = rows.Scan(&id, &name); err != nil {
				closeErr := rows.Close()
				return errors.Join(err, closeErr)
			}
			out[id] = name
		}
		err = errors.Join(rows.Err(), rows.Close())
		if err != nil {
			return err
		}
		rows, err = tx.QueryContext(ctx, "SELECT alias,canonical FROM aliases")
		if err != nil {
			return err
		}
		defer rows.Close() //nolint:errcheck // Explicit closure or rows.Err below reports the error.
		for rows.Next() {
			var id, target string
			if err = rows.Scan(&id, &target); err != nil {
				closeErr := rows.Close()
				return errors.Join(err, closeErr)
			}
			if out[target] != "" {
				out[id] = out[target]
			}
		}
		err = errors.Join(rows.Err(), rows.Close())
		if err != nil {
			return err
		}
		var self string
		err = tx.QueryRowContext(ctx, "SELECT value FROM meta WHERE key='account'").Scan(&self)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		out["self_id"] = self
		out["self_name"] = "Você"
		if out[self] != "" {
			out["self_name"] = out[self] + " (você)"
		}
		return nil
	})()
	return out, err
}

func personLabel(names map[string]string, id, fallback string) string {
	if names[id] != "" {
		return names[id]
	}
	if fallback != "" {
		return fallback
	}
	if id != "" {
		return id
	}
	return "Autor desconhecido"
}
func route(m Message, names map[string]string) string {
	sender := personLabel(names, m.Sender, m.Author)
	if m.FromMe && names[m.Sender] == "" && m.Author == "" {
		sender = "Você"
	}
	destination := personLabel(names, m.Chat, "")
	if m.Group {
		destination = "grupo " + destination
	} else if !m.FromMe {
		destination = names["self_name"]
		if destination == "" {
			destination = "Você"
		}
	}
	return sender + " → " + destination
}
