package tp

import (
	"database/sql"
	"fmt"
)

func Bad(db *sql.DB, id string) {
	db.Query("SELECT * FROM t WHERE id = " + id) // want "SQL built from string"
}

func BadFmt(db *sql.DB, id string) {
	db.Exec(fmt.Sprintf("DELETE FROM t WHERE id=%s", id)) // want "SQL built from string"
}
