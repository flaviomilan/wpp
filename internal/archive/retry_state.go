package archive

import "database/sql"

// AudioRetryState contains domain facts only, without message text or display labels.
type AudioRetryState struct {
	Source, Chat, State                          string
	Tracked, HasMetadata, Deleted, RetryEligible bool
}

func (s *Store) AudioRetryState(source string) (AudioRetryState, error) {
	var state AudioRetryState
	err := s.transaction(func(tx *sql.Tx) error {
		id, err := resolveSource(tx, source)
		if err != nil {
			return err
		}
		state.Source = id
		var detail string
		err = tx.QueryRow("SELECT a.chat,a.state,a.error,c.tracked,(a.metadata IS NOT NULL AND length(a.metadata)>0),COALESCE(json_extract(m.data,'$.Deleted'),0) FROM audio_jobs a JOIN messages m ON m.source_id=a.source_id JOIN chats c ON c.id=a.chat WHERE a.source_id=?", id).Scan(&state.Chat, &state.State, &detail, &state.Tracked, &state.HasMetadata, &state.Deleted)
		state.RetryEligible = state.HasMetadata && !state.Deleted && (state.State == "failed" || (state.State == "pending" && detail != ""))
		return err
	})
	return state, err
}
