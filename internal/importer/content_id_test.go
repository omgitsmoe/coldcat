package importer

import (
	"strings"
	"testing"
)

func TestNewContentIDSurvivesInsertTriggerAndExistingContentConflict(t *testing.T) {
	db, raw, disk := testDB(t)
	assertNoErr(t, execSQL(raw, `CREATE TABLE content_audit(id INTEGER PRIMARY KEY);
 CREATE TRIGGER audit_content AFTER INSERT ON content BEGIN
 INSERT INTO content_audit(id) VALUES(NEW.id+10000); END;`))
	_, err := ImportReader(t.Context(), db, request(disk),
		strings.NewReader("# version 1\n,4,sha256,"+fixtureSHA256AB+" original\n"))
	assertNoErr(t, err)
	input := "# version 1\n,7,sha256," + fixtureSHA25600000000 + " new\n" +
		",4,sha256," + fixtureSHA256AB + " existing\n" +
		",7,sha256," + fixtureSHA25600000000 + " repeated-new\n"
	result, err := ImportReader(t.Context(), db, request(disk), strings.NewReader(input))
	assertNoErr(t, err)
	assertEqual(t, result.FileCount, int64(3))
	assertEqual(t, result.ContentCount, int64(2))
	assertEqual(t, count(t, raw, "content"), 2)
	assertEqual(t, count(t, raw, "content_audit"), 2)
	for _, test := range []struct {
		path string
		id   int64
		size int64
	}{
		{"new", 2, 7}, {"existing", 1, 4}, {"repeated-new", 2, 7},
	} {
		var id, size int64
		assertNoErr(t, raw.QueryRow(`SELECT o.content_id,c.size FROM observation o
 JOIN content c ON c.id=o.content_id WHERE o.snapshot_id=? AND o.path=?`, result.Id, test.path).
			Scan(&id, &size))
		assertEqual(t, id, test.id)
		assertEqual(t, size, test.size)
	}
}
