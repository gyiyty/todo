package app

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

type List struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Color    string `json:"color"`
	Position int    `json:"position"`
}

type Tag struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type Reminder struct {
	ID            string  `json:"id"`
	Kind          string  `json:"kind"`
	OffsetMinutes *int    `json:"offset_minutes,omitempty"`
	TriggerAt     string  `json:"trigger_at"`
	SentAt        *string `json:"sent_at,omitempty"`
}

type Task struct {
	ID                 string     `json:"id"`
	Title              string     `json:"title"`
	Notes              string     `json:"notes"`
	ListID             *string    `json:"list_id,omitempty"`
	List               *List      `json:"list,omitempty"`
	DueAt              *string    `json:"due_at,omitempty"`
	Priority           int        `json:"priority"`
	Done               bool       `json:"done"`
	CompletedAt        *string    `json:"completed_at,omitempty"`
	Archived           bool       `json:"archived"`
	RecurrenceUnit     string     `json:"recurrence_unit"`
	RecurrenceInterval int        `json:"recurrence_interval"`
	Tags               []Tag      `json:"tags"`
	Reminders          []Reminder `json:"reminders"`
	CreatedAt          string     `json:"created_at"`
	UpdatedAt          string     `json:"updated_at"`
}

type reminderInput struct {
	Kind          string  `json:"kind"`
	OffsetMinutes *int    `json:"offset_minutes"`
	TriggerAt     *string `json:"trigger_at"`
}

type taskInput struct {
	Title              *string          `json:"title"`
	Notes              *string          `json:"notes"`
	ListID             *string          `json:"list_id"`
	DueAt              *string          `json:"due_at"`
	Priority           *int             `json:"priority"`
	Archived           *bool            `json:"archived"`
	RecurrenceUnit     *string          `json:"recurrence_unit"`
	RecurrenceInterval *int             `json:"recurrence_interval"`
	TagIDs             *[]string        `json:"tag_ids"`
	Reminders          *[]reminderInput `json:"reminders"`
}

func (s *Server) dashboard(w http.ResponseWriter, _ *http.Request) {
	todayStart := time.Now().In(s.cfg.Timezone).Truncate(24 * time.Hour)
	// Truncate is duration-based; reconstruct local midnight to handle timezone offsets.
	nowLocal := time.Now().In(s.cfg.Timezone)
	todayStart = time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), 0, 0, 0, 0, s.cfg.Timezone)
	tomorrow := todayStart.AddDate(0, 0, 1)
	week := todayStart.AddDate(0, 0, 7)
	result := map[string]int{}
	queries := map[string]struct {
		query string
		args  []any
	}{
		"inbox":    {"SELECT COUNT(*) FROM tasks WHERE done = 0 AND archived = 0 AND list_id IS NULL", nil},
		"today":    {"SELECT COUNT(*) FROM tasks WHERE done = 0 AND archived = 0 AND due_at < ?", []any{tomorrow.UTC().Format(timeFormat)}},
		"upcoming": {"SELECT COUNT(*) FROM tasks WHERE done = 0 AND archived = 0 AND due_at >= ? AND due_at < ?", []any{tomorrow.UTC().Format(timeFormat), week.UTC().Format(timeFormat)}},
		"all":      {"SELECT COUNT(*) FROM tasks WHERE done = 0 AND archived = 0", nil},
	}
	for key, item := range queries {
		var count int
		_ = s.db.QueryRow(item.query, item.args...).Scan(&count)
		result[key] = count
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) listLists(w http.ResponseWriter, _ *http.Request) {
	rows, err := s.db.Query("SELECT id, name, color, position FROM lists ORDER BY position, name")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load lists")
		return
	}
	defer rows.Close()
	items := []List{}
	for rows.Next() {
		var item List
		if err := rows.Scan(&item.ID, &item.Name, &item.Color, &item.Position); err == nil {
			items = append(items, item)
		}
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) createList(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 80 {
		writeError(w, http.StatusBadRequest, "list name is required and must be at most 80 characters")
		return
	}
	if input.Color == "" {
		input.Color = "#357266"
	}
	id, now := newID("lst"), nowString()
	_, err := s.db.Exec("INSERT INTO lists(id, name, color, position, created_at, updated_at) VALUES(?, ?, ?, COALESCE((SELECT MAX(position) + 1 FROM lists), 0), ?, ?)", id, input.Name, input.Color, now, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create list")
		return
	}
	writeJSON(w, http.StatusCreated, List{ID: id, Name: input.Name, Color: input.Color})
}

func (s *Server) updateList(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name     *string `json:"name"`
		Color    *string `json:"color"`
		Position *int    `json:"position"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	result, err := s.db.Exec("UPDATE lists SET name = COALESCE(?, name), color = COALESCE(?, color), position = COALESCE(?, position), updated_at = ? WHERE id = ?", input.Name, input.Color, input.Position, nowString(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not update list")
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		writeError(w, http.StatusNotFound, "list not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteList(w http.ResponseWriter, r *http.Request) {
	result, err := s.db.Exec("DELETE FROM lists WHERE id = ?", chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete list")
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		writeError(w, http.StatusNotFound, "list not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listTags(w http.ResponseWriter, _ *http.Request) {
	rows, err := s.db.Query("SELECT id, name, color FROM tags ORDER BY name")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load tags")
		return
	}
	defer rows.Close()
	items := []Tag{}
	for rows.Next() {
		var item Tag
		if rows.Scan(&item.ID, &item.Name, &item.Color) == nil {
			items = append(items, item)
		}
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) createTag(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 40 {
		writeError(w, http.StatusBadRequest, "tag name is required and must be at most 40 characters")
		return
	}
	if input.Color == "" {
		input.Color = "#687078"
	}
	item := Tag{ID: newID("tag"), Name: input.Name, Color: input.Color}
	if _, err := s.db.Exec("INSERT INTO tags(id, name, color, created_at) VALUES(?, ?, ?, ?)", item.ID, item.Name, item.Color, nowString()); err != nil {
		writeError(w, http.StatusConflict, "tag already exists")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) deleteTag(w http.ResponseWriter, r *http.Request) {
	result, err := s.db.Exec("DELETE FROM tags WHERE id = ?", chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete tag")
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		writeError(w, http.StatusNotFound, "tag not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	query := `SELECT t.id, t.title, t.notes, t.list_id, t.due_at, t.priority, t.done, t.completed_at, t.archived,
t.recurrence_unit, t.recurrence_interval, t.created_at, t.updated_at, l.id, l.name, l.color, l.position
FROM tasks t LEFT JOIN lists l ON l.id = t.list_id WHERE 1 = 1`
	args := []any{}
	view := r.URL.Query().Get("view")
	if view != "completed" && view != "all-including-completed" {
		query += " AND t.done = 0 AND t.archived = 0"
	} else if view == "completed" {
		query += " AND t.done = 1 AND t.archived = 0"
	}
	if listID := r.URL.Query().Get("list_id"); listID != "" {
		if listID == "inbox" {
			query += " AND t.list_id IS NULL"
		} else {
			query += " AND t.list_id = ?"
			args = append(args, listID)
		}
	}
	if search := strings.TrimSpace(r.URL.Query().Get("q")); search != "" {
		query += " AND (t.title LIKE ? ESCAPE '\\' OR t.notes LIKE ? ESCAPE '\\')"
		search = "%" + strings.NewReplacer("%", "\\%", "_", "\\_").Replace(search) + "%"
		args = append(args, search, search)
	}
	if view == "today" || view == "upcoming" {
		now := time.Now().In(s.cfg.Timezone)
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.cfg.Timezone)
		if view == "today" {
			query += " AND t.due_at < ?"
			args = append(args, start.AddDate(0, 0, 1).UTC().Format(timeFormat))
		} else {
			query += " AND t.due_at >= ? AND t.due_at < ?"
			args = append(args, start.AddDate(0, 0, 1).UTC().Format(timeFormat), start.AddDate(0, 0, 7).UTC().Format(timeFormat))
		}
	}
	if priority := r.URL.Query().Get("priority"); priority != "" {
		if parsed, err := strconv.Atoi(priority); err == nil {
			query += " AND t.priority = ?"
			args = append(args, parsed)
		}
	}
	query += " ORDER BY t.done, CASE WHEN t.due_at IS NULL THEN 1 ELSE 0 END, t.due_at, t.priority DESC, t.created_at DESC LIMIT 500"
	rows, err := s.db.Query(query, args...)
	if err != nil {
		s.logger.Error("list tasks", "error", err)
		writeError(w, http.StatusInternalServerError, "could not load tasks")
		return
	}
	defer rows.Close()
	items := []Task{}
	for rows.Next() {
		item, err := scanTask(rows)
		if err == nil {
			items = append(items, item)
		}
	}
	for i := range items {
		s.loadTaskRelations(&items[i])
	}
	writeJSON(w, http.StatusOK, items)
}

type scanner interface{ Scan(...any) error }

func scanTask(row scanner) (Task, error) {
	var item Task
	var listID, dueAt, completedAt, joinedListID, listName, listColor sql.NullString
	var listPosition sql.NullInt64
	var done, archived int
	err := row.Scan(&item.ID, &item.Title, &item.Notes, &listID, &dueAt, &item.Priority, &done, &completedAt, &archived,
		&item.RecurrenceUnit, &item.RecurrenceInterval, &item.CreatedAt, &item.UpdatedAt, &joinedListID, &listName, &listColor, &listPosition)
	if err != nil {
		return item, err
	}
	item.Done, item.Archived = done == 1, archived == 1
	if listID.Valid {
		item.ListID = &listID.String
	}
	if dueAt.Valid {
		item.DueAt = &dueAt.String
	}
	if completedAt.Valid {
		item.CompletedAt = &completedAt.String
	}
	if joinedListID.Valid {
		item.List = &List{ID: joinedListID.String, Name: listName.String, Color: listColor.String, Position: int(listPosition.Int64)}
	}
	item.Tags, item.Reminders = []Tag{}, []Reminder{}
	return item, nil
}

func (s *Server) loadTask(id string) (Task, error) {
	row := s.db.QueryRow(`SELECT t.id, t.title, t.notes, t.list_id, t.due_at, t.priority, t.done, t.completed_at, t.archived,
t.recurrence_unit, t.recurrence_interval, t.created_at, t.updated_at, l.id, l.name, l.color, l.position
FROM tasks t LEFT JOIN lists l ON l.id = t.list_id WHERE t.id = ?`, id)
	item, err := scanTask(row)
	if err == nil {
		s.loadTaskRelations(&item)
	}
	return item, err
}

func (s *Server) loadTaskRelations(item *Task) {
	tagRows, err := s.db.Query("SELECT g.id, g.name, g.color FROM tags g JOIN task_tags tt ON tt.tag_id = g.id WHERE tt.task_id = ? ORDER BY g.name", item.ID)
	if err == nil {
		defer tagRows.Close()
		for tagRows.Next() {
			var tag Tag
			if tagRows.Scan(&tag.ID, &tag.Name, &tag.Color) == nil {
				item.Tags = append(item.Tags, tag)
			}
		}
	}
	reminderRows, err := s.db.Query("SELECT id, kind, offset_minutes, trigger_at, sent_at FROM reminders WHERE task_id = ? ORDER BY trigger_at", item.ID)
	if err == nil {
		defer reminderRows.Close()
		for reminderRows.Next() {
			var reminder Reminder
			var offset sql.NullInt64
			var sent sql.NullString
			if reminderRows.Scan(&reminder.ID, &reminder.Kind, &offset, &reminder.TriggerAt, &sent) == nil {
				if offset.Valid {
					value := int(offset.Int64)
					reminder.OffsetMinutes = &value
				}
				if sent.Valid {
					reminder.SentAt = &sent.String
				}
				item.Reminders = append(item.Reminders, reminder)
			}
		}
	}
}

func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	item, err := s.loadTask(chi.URLParam(r, "id"))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load task")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	var input taskInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.applyTaskInput("", input, true)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) updateTask(w http.ResponseWriter, r *http.Request) {
	var input taskInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.applyTaskInput(chi.URLParam(r, "id"), input, false)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) applyTaskInput(id string, input taskInput, create bool) (Task, error) {
	var current Task
	var err error
	if create {
		current = Task{ID: newID("tsk"), Priority: 0, RecurrenceInterval: 1, Tags: []Tag{}, Reminders: []Reminder{}}
	} else {
		current, err = s.loadTask(id)
		if err != nil {
			return current, err
		}
	}
	if input.Title != nil {
		current.Title = strings.TrimSpace(*input.Title)
	}
	if current.Title == "" || len(current.Title) > 300 {
		return current, errors.New("title is required and must be at most 300 characters")
	}
	if input.Notes != nil {
		if len(*input.Notes) > 20000 {
			return current, errors.New("notes must be at most 20000 characters")
		}
		current.Notes = *input.Notes
	}
	if input.ListID != nil {
		value := strings.TrimSpace(*input.ListID)
		if value == "" {
			current.ListID = nil
		} else {
			current.ListID = &value
		}
	}
	if input.DueAt != nil {
		parsed, parseErr := parseOptionalTime(input.DueAt)
		if parseErr != nil {
			return current, parseErr
		}
		if parsed == nil {
			current.DueAt = nil
		} else {
			value := parsed.Format(timeFormat)
			current.DueAt = &value
		}
	}
	if input.Priority != nil {
		if *input.Priority < 0 || *input.Priority > 3 {
			return current, errors.New("priority must be between 0 and 3")
		}
		current.Priority = *input.Priority
	}
	if input.Archived != nil {
		current.Archived = *input.Archived
	}
	if input.RecurrenceUnit != nil {
		unit := *input.RecurrenceUnit
		if unit != "" && unit != "day" && unit != "week" && unit != "month" && unit != "year" {
			return current, errors.New("invalid recurrence unit")
		}
		current.RecurrenceUnit = unit
	}
	if input.RecurrenceInterval != nil {
		if *input.RecurrenceInterval < 1 || *input.RecurrenceInterval > 365 {
			return current, errors.New("recurrence interval must be between 1 and 365")
		}
		current.RecurrenceInterval = *input.RecurrenceInterval
	}
	if current.RecurrenceUnit != "" && current.DueAt == nil {
		return current, errors.New("a recurring task requires a due date")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return current, err
	}
	defer tx.Rollback()
	now := nowString()
	if create {
		current.CreatedAt = now
		_, err = tx.Exec(`INSERT INTO tasks(id, title, notes, list_id, due_at, priority, archived, recurrence_unit, recurrence_interval, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, current.ID, current.Title, current.Notes, current.ListID, current.DueAt, current.Priority, boolInt(current.Archived), current.RecurrenceUnit, current.RecurrenceInterval, now, now)
	} else {
		_, err = tx.Exec(`UPDATE tasks SET title=?, notes=?, list_id=?, due_at=?, priority=?, archived=?, recurrence_unit=?, recurrence_interval=?, updated_at=? WHERE id=?`,
			current.Title, current.Notes, current.ListID, current.DueAt, current.Priority, boolInt(current.Archived), current.RecurrenceUnit, current.RecurrenceInterval, now, current.ID)
	}
	if err != nil {
		return current, fmt.Errorf("could not save task: %w", err)
	}
	if input.TagIDs != nil {
		if _, err = tx.Exec("DELETE FROM task_tags WHERE task_id = ?", current.ID); err != nil {
			return current, err
		}
		for _, tagID := range *input.TagIDs {
			if _, err = tx.Exec("INSERT INTO task_tags(task_id, tag_id) VALUES(?, ?)", current.ID, tagID); err != nil {
				return current, errors.New("invalid tag")
			}
		}
	}
	if input.Reminders != nil {
		if _, err = tx.Exec("DELETE FROM reminders WHERE task_id = ?", current.ID); err != nil {
			return current, err
		}
		for _, reminder := range *input.Reminders {
			trigger, offset, validationErr := reminderTrigger(reminder, current.DueAt)
			if validationErr != nil {
				return current, validationErr
			}
			_, err = tx.Exec("INSERT INTO reminders(id, task_id, kind, offset_minutes, trigger_at, created_at) VALUES(?, ?, ?, ?, ?, ?)", newID("rem"), current.ID, reminder.Kind, offset, trigger, now)
			if err != nil {
				return current, err
			}
		}
	} else if !create && input.DueAt != nil {
		// Relative reminders track a changed due date and become eligible again.
		rows, queryErr := tx.Query("SELECT id, offset_minutes FROM reminders WHERE task_id = ? AND kind = 'relative'", current.ID)
		if queryErr != nil {
			return current, queryErr
		}
		type relativeUpdate struct {
			id     string
			offset int
		}
		updates := []relativeUpdate{}
		for rows.Next() {
			var update relativeUpdate
			if rows.Scan(&update.id, &update.offset) == nil {
				updates = append(updates, update)
			}
		}
		rows.Close()
		if current.DueAt == nil && len(updates) > 0 {
			return current, errors.New("cannot remove due date while relative reminders exist")
		}
		if current.DueAt != nil {
			due, _ := time.Parse(timeFormat, *current.DueAt)
			for _, update := range updates {
				if _, err = tx.Exec("UPDATE reminders SET trigger_at=?, sent_at=NULL WHERE id=?", due.Add(time.Duration(update.offset)*time.Minute).Format(timeFormat), update.id); err != nil {
					return current, err
				}
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return current, err
	}
	return s.loadTask(current.ID)
}

func reminderTrigger(input reminderInput, dueAt *string) (string, any, error) {
	if input.Kind == "relative" {
		if dueAt == nil || input.OffsetMinutes == nil {
			return "", nil, errors.New("relative reminders require a due date and offset_minutes")
		}
		due, _ := time.Parse(timeFormat, *dueAt)
		trigger := due.Add(time.Duration(*input.OffsetMinutes) * time.Minute)
		return trigger.Format(timeFormat), *input.OffsetMinutes, nil
	}
	if input.Kind == "absolute" {
		parsed, err := parseOptionalTime(input.TriggerAt)
		if err != nil || parsed == nil {
			return "", nil, errors.New("absolute reminders require trigger_at")
		}
		return parsed.Format(timeFormat), nil, nil
	}
	return "", nil, errors.New("reminder kind must be absolute or relative")
}

func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) {
	result, err := s.db.Exec("DELETE FROM tasks WHERE id = ?", chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete task")
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) completeTask(w http.ResponseWriter, r *http.Request) {
	item, err := s.loadTask(chi.URLParam(r, "id"))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load task")
		return
	}
	if item.Done {
		writeJSON(w, http.StatusOK, item)
		return
	}
	now := nowString()
	if _, err := s.db.Exec("UPDATE tasks SET done=1, completed_at=?, updated_at=? WHERE id=?", now, now, item.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not complete task")
		return
	}
	var next *Task
	if item.RecurrenceUnit != "" && item.DueAt != nil {
		created, createErr := s.createNextOccurrence(item)
		if createErr != nil {
			s.logger.Error("create recurring task", "task_id", item.ID, "error", createErr)
		} else {
			next = &created
		}
	}
	item, _ = s.loadTask(item.ID)
	writeJSON(w, http.StatusOK, map[string]any{"task": item, "next": next})
}

func (s *Server) createNextOccurrence(item Task) (Task, error) {
	due, _ := time.Parse(timeFormat, *item.DueAt)
	n := item.RecurrenceInterval
	var nextDue time.Time
	switch item.RecurrenceUnit {
	case "day":
		nextDue = due.AddDate(0, 0, n)
	case "week":
		nextDue = due.AddDate(0, 0, 7*n)
	case "month":
		nextDue = due.AddDate(0, n, 0)
	case "year":
		nextDue = due.AddDate(n, 0, 0)
	}
	input := taskInput{Title: &item.Title, Notes: &item.Notes, ListID: pointerOrEmpty(item.ListID), DueAt: stringPointer(nextDue.Format(timeFormat)), Priority: &item.Priority, RecurrenceUnit: &item.RecurrenceUnit, RecurrenceInterval: &item.RecurrenceInterval}
	tagIDs := []string{}
	for _, tag := range item.Tags {
		tagIDs = append(tagIDs, tag.ID)
	}
	input.TagIDs = &tagIDs
	reminders := []reminderInput{}
	delta := nextDue.Sub(due)
	for _, reminder := range item.Reminders {
		if reminder.Kind == "relative" {
			reminders = append(reminders, reminderInput{Kind: "relative", OffsetMinutes: reminder.OffsetMinutes})
		} else {
			trigger, _ := time.Parse(timeFormat, reminder.TriggerAt)
			value := trigger.Add(delta).Format(timeFormat)
			reminders = append(reminders, reminderInput{Kind: "absolute", TriggerAt: &value})
		}
	}
	input.Reminders = &reminders
	return s.applyTaskInput("", input, true)
}

func (s *Server) reopenTask(w http.ResponseWriter, r *http.Request) {
	result, err := s.db.Exec("UPDATE tasks SET done=0, completed_at=NULL, updated_at=? WHERE id=?", nowString(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not reopen task")
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	item, _ := s.loadTask(chi.URLParam(r, "id"))
	writeJSON(w, http.StatusOK, item)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func stringPointer(value string) *string { return &value }

func pointerOrEmpty(value *string) *string {
	if value == nil {
		return stringPointer("")
	}
	return value
}
