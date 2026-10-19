package archive

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func TestMessagePreviewsAndAmbiguousReplies(t *testing.T) {
	s := testStore(t)
	must(t, s.Track("group@g.us", true))
	a := message("shared", 1)
	b := a
	b.Sender = "other@lid"
	reply := message("reply", 2)
	reply.ReplyID = "shared"
	reply.Text = strings.Repeat("ação 👩🏽‍💻\n", 2000)
	ingest(t, s, a, b, reply)
	page, err := s.BrowseMessages(a.Chat, "", "", 0, 50)
	must(t, err)
	if page.Items[0].ReplyAuthor != "" {
		t.Fatal("ambiguous reply assigned")
	}
	if !page.Items[0].PreviewTruncated || len(page.Items[0].Message.Text) > 12000 || !utf8.ValidString(page.Items[0].Message.Text) {
		t.Fatal("invalid UTF-8 preview")
	}
	_, messages, _, err := s.Snapshot(a.Chat)
	must(t, err)
	if len(messages[2].Text) != len(reply.Text) {
		t.Fatal("preview truncated source")
	}
	names := map[string]string{"5511@s.whatsapp.net": "Contato", "self_name": "Você"}
	direct := Message{Chat: "5511@s.whatsapp.net", Sender: "5511@s.whatsapp.net"}
	if route(direct, names) != "Contato → Você" {
		t.Fatal("incoming direct recipient")
	}
	direct.FromMe = true
	direct.Sender = ""
	if route(direct, names) != "Você → Contato" {
		t.Fatal("outgoing direct recipient")
	}
}

func TestRegistryKeepsPendingBatchImmutable(t *testing.T) {
	s, b := testBatches(t)
	ingest(t, s, message("one", 1))
	path, manifest, original := nextLot(t, b)
	must(t, s.SetPersonName("group@g.us", "person@s.whatsapp.net", "Fernanda"))
	same, _, updated := nextLot(t, b)
	if same != path || !bytes.Equal(original, updated) {
		t.Fatal("name registration changed published batch")
	}
	must(t, b.Ack("group@g.us", manifest.BatchID))
}

func TestRegistryMigrationAndCaptureConcurrency(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	must(t, err)
	must(t, s.Track("group@g.us", true))
	ingest(t, s, message("before", 1))
	before := changesFor(t, s)
	_, err = s.db.Exec("DROP TABLE group_people; DROP TABLE people_names; PRAGMA user_version=2")
	must(t, err)
	must(t, s.Close())
	s, err = Open(dir)
	must(t, err)
	defer func() { must(t, s.Close()) }()
	after := changesFor(t, s)
	if len(after) != len(before) || after[0].Source != before[0].Source {
		t.Fatal("migration changed evidence")
	}
	people, err := s.People("group@g.us")
	must(t, err)
	if len(people.People) != 1 {
		t.Fatal("missing migrated people")
	}
	capture, err := Open(dir)
	must(t, err)
	defer func() { must(t, capture.Close()) }()
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := int64(2); i < 15; i++ {
			if _, e := capture.Ingest([]Message{message(time.Unix(i, 0).String(), i)}); e != nil {
				errs <- e
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			if e := s.SetPersonName("group@g.us", "person@s.whatsapp.net", "Fernanda"); e != nil {
				errs <- e
				return
			}
		}
	}()
	wg.Wait()
	close(errs)
	for e := range errs {
		must(t, e)
	}
	page, err := s.BrowseMessages("group@g.us", "", "", 0, 50)
	must(t, err)
	if len(page.Items) != 14 || page.Items[0].Author != "Fernanda" {
		t.Fatal("concurrent data lost", page)
	}
}

func TestPersonNamesPreserveEvidenceAndResolveReplies(t *testing.T) {
	s := testStore(t)
	must(t, s.Track("group@g.us", true))
	must(t, s.Discover("group@g.us", "Equipe"))
	original := message("first", 1)
	original.Author = "Nome recebido"
	reply := message("reply", 2)
	reply.Sender = "other@lid"
	reply.ReplyID = original.ID
	reply.Text = "Confirmado ✅"
	ingest(t, s, original, reply)
	before, err := s.BrowseMessages(original.Chat, "", "", 0, 50)
	must(t, err)
	must(t, s.SetPersonName(original.Chat, original.Sender, "Maria [Financeiro]"))
	after, err := s.BrowseMessages(original.Chat, "", "", 0, 50)
	must(t, err)
	if after.Items[0].ReplyAuthor != "Maria [Financeiro]" || after.Items[1].Route != "Maria [Financeiro] → grupo Equipe" {
		t.Fatalf("display: %+v", after.Items)
	}
	if before.Items[1].SourceID != after.Items[1].SourceID || after.Items[1].Message.Author != original.Author {
		t.Fatal("name changed evidence")
	}
	var count int
	must(t, s.db.QueryRow("SELECT COUNT(*) FROM changes").Scan(&count))
	if count != 2 {
		t.Fatal("registry must not generate message revisions")
	}
	people, err := s.People(original.Chat)
	must(t, err)
	if len(people.People) != 2 {
		t.Fatal(people)
	}
	must(t, s.SetPersonName(original.Chat, original.Sender, ""))
	after, err = s.BrowseMessages(original.Chat, "", "", 0, 50)
	must(t, err)
	if after.Items[1].Author != original.Author {
		t.Fatal("name fallback lost")
	}
}

func TestPersonAliasesAndAtomicRegistration(t *testing.T) {
	s := testStore(t)
	group := "group@g.us"
	must(t, s.Track(group, true))
	must(t, s.ObservePerson(group, "123@lid", "Recebido"))
	must(t, s.SetPersonName(group, "123@lid", "Ana"))
	must(t, s.Alias("123@lid", "5511@s.whatsapp.net"))
	people, err := s.People(group)
	must(t, err)
	if len(people.People) != 1 || people.People[0].ID != "5511@s.whatsapp.net" || people.People[0].Name != "Ana" {
		t.Fatal(people)
	}
	_, err = s.RegisterPeople(PeopleFile{SchemaVersion: 1, GroupID: group, People: []Person{{ID: "123@lid", Name: "Mudança"}, {ID: "bad", Name: "Inválido"}}})
	if err == nil {
		t.Fatal("invalid ID accepted")
	}
	people, err = s.People(group)
	must(t, err)
	if people.People[0].Name != "Ana" {
		t.Fatal("partial registration")
	}
	must(t, s.SetPersonName(group, "123@lid", ""))
	names, err := s.Names()
	must(t, err)
	if names["5511@s.whatsapp.net"] == "Ana" {
		t.Fatal("alias name not cleared")
	}
}

func TestBrowserPaginationAndLiteralFilters(t *testing.T) {
	s := testStore(t)
	must(t, s.Track("group@g.us", true))
	a := message("a", 1)
	a.Text = "100% 👩🏽‍💻"
	b := message("b", 2)
	b.Text = "1000"
	ingest(t, s, a, b)
	page, err := s.BrowseMessages(a.Chat, "%", "", 0, 50)
	must(t, err)
	if len(page.Items) != 1 || page.Items[0].Message.ID != "a" {
		t.Fatal(page)
	}
	page, err = s.BrowseMessages(a.Chat, "", "", 0, 1)
	must(t, err)
	if !page.More || page.Items[0].Message.ID != "b" {
		t.Fatal(page)
	}
	page, err = s.BrowseMessages(a.Chat, "", "", 1, 1)
	must(t, err)
	if page.More || page.Items[0].Message.ID != "a" {
		t.Fatal(page)
	}
	if _, err = s.InspectTable("messages; DROP TABLE messages", 0, 50); err == nil {
		t.Fatal("arbitrary SQL accepted")
	}
	if _, err = s.InspectTable("sqlite_master", 0, 50); err == nil {
		t.Fatal("non allowlisted table")
	}
	must(t, s.SetPersonName(a.Chat, a.Sender, "<script>alert('x')</script>"))
	if err = s.SetPersonName(a.Chat, a.Sender, "a\nb"); err == nil {
		t.Fatal("control character accepted")
	}
}
