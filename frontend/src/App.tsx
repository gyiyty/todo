import { useEffect, useMemo, useState } from 'preact/hooks'
import {
  Archive, Bell, CalendarDays, Check, CheckCircle2, ChevronLeft, Circle, Clock3,
  FilePenLine, Inbox, KeyRound, LayoutGrid, List as ListIcon, LogOut, Menu,
  MoreHorizontal, Plus, RefreshCw, Repeat2, Save, Search, Settings, SlidersHorizontal, Tag as TagIcon,
  Trash2, Webhook, X
} from 'lucide-preact'
import { api, APIError, del, patch, post, put } from './api'
import type { Delivery, List, Notification, Reminder, Tag, Task, TaskInput, TokenSummary, User } from './types'

type View = 'today' | 'inbox' | 'upcoming' | 'all' | 'matrix' | 'completed' | 'notifications' | 'settings' | `list:${string}`

const viewLabels: Record<string, string> = {
  today: '今天', inbox: '收件箱', upcoming: '近期', all: '全部任务', matrix: '四象限', completed: '已完成', notifications: '提醒通知', settings: '设置'
}

const priorityLabels = ['无', '低', '中', '高']
const recurrenceLabels: Record<string, string> = { '': '不重复', day: '天', week: '周', month: '月', year: '年' }

function toLocalInput(value?: string) {
  if (!value) return ''
  const date = new Date(value)
  const shifted = new Date(date.getTime() - date.getTimezoneOffset() * 60000)
  return shifted.toISOString().slice(0, 16)
}

function toISOString(value: string) { return value ? new Date(value).toISOString() : '' }

function formatDate(value?: string, withTime = true) {
  if (!value) return ''
  return new Intl.DateTimeFormat('zh-CN', withTime ? { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' } : { year: 'numeric', month: 'short', day: 'numeric' }).format(new Date(value))
}

function readError(error: unknown) { return error instanceof Error ? error.message : '发生未知错误' }

function dateKey(value: Date, timeZone: string) {
  const parts = new Intl.DateTimeFormat('en-US', { timeZone, year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(value)
  const part = (type: Intl.DateTimeFormatPartTypes) => parts.find(item => item.type === type)?.value || ''
  return `${part('year')}-${part('month')}-${part('day')}`
}

export function App() {
  const [user, setUser] = useState<User | null | undefined>(undefined)

  useEffect(() => {
    api<User>('/me').then(setUser).catch(error => {
      if (error instanceof APIError && error.status === 401) setUser(null)
      else setUser(null)
    })
  }, [])

  if (user === undefined) return <div class="splash"><CheckCircle2 size={34} /><span>正在载入待办…</span></div>
  if (user === null) return <Login onLogin={setUser} />
  return <Workspace user={user} onLogout={() => setUser(null)} />
}

function Login({ onLogin }: { onLogin: (user: User) => void }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  async function submit(event: Event) {
    event.preventDefault(); setBusy(true); setError('')
    try {
      await post('/session', { username, password })
      onLogin(await api<User>('/me'))
    } catch (err) { setError(readError(err)) }
    finally { setBusy(false) }
  }

  return <main class="login-page">
    <section class="login-panel">
      <div class="brand-mark"><Check size={30} strokeWidth={3} /></div>
      <div><p class="eyebrow">PERSONAL TASKS</p><h1>拾光待办</h1><p class="muted">把今天要做的事，稳稳放在这里。</p></div>
      <form onSubmit={submit} class="login-form">
        <label>用户名<input value={username} onInput={e => setUsername(e.currentTarget.value)} autocomplete="username" required autofocus /></label>
        <label>密码<input type="password" value={password} onInput={e => setPassword(e.currentTarget.value)} autocomplete="current-password" required /></label>
        {error && <div class="form-error" role="alert">{error}</div>}
        <button class="primary wide" disabled={busy}>{busy ? '正在登录…' : '登录'}</button>
      </form>
    </section>
    <aside class="login-aside"><div><span>今天</span><strong>{new Intl.DateTimeFormat('zh-CN', { month: 'long', day: 'numeric' }).format(new Date())}</strong><p>一次只做一件重要的事。</p></div></aside>
  </main>
}

function Workspace({ user, onLogout }: { user: User; onLogout: () => void }) {
  const [view, setView] = useState<View>('today')
  const [lists, setLists] = useState<List[]>([])
  const [tags, setTags] = useState<Tag[]>([])
  const [tasks, setTasks] = useState<Task[]>([])
  const [counts, setCounts] = useState<Record<string, number>>({})
  const [selected, setSelected] = useState<Task | null>(null)
  const [creating, setCreating] = useState(false)
  const [creationDraft, setCreationDraft] = useState({ title: '', listID: '' })
  const [search, setSearch] = useState('')
  const [searchOpen, setSearchOpen] = useState(false)
  const [priorityFilter, setPriorityFilter] = useState('')
  const [tagFilters, setTagFilters] = useState<string[]>([])
  const [loading, setLoading] = useState(true)
  const [menuOpen, setMenuOpen] = useState(false)
  const [notice, setNotice] = useState('')

  const selectedList = view.startsWith('list:') ? lists.find(item => item.id === view.slice(5)) : undefined
  const title = selectedList?.name || viewLabels[view] || '任务'

  async function loadMeta() {
    const [loadedLists, loadedTags, loadedCounts] = await Promise.all([
      api<List[]>('/lists'), api<Tag[]>('/tags'), api<Record<string, number>>('/dashboard')
    ])
    setLists(loadedLists); setTags(loadedTags); setCounts(loadedCounts)
  }

  async function loadTasks() {
    if (view === 'settings' || view === 'notifications') return
    setLoading(true)
    const params = new URLSearchParams()
    if (view.startsWith('list:')) params.set('list_id', view.slice(5))
    else if (view === 'inbox') params.set('list_id', 'inbox')
    else params.set('view', view)
    if (search.trim()) params.set('q', search.trim())
    if (priorityFilter) params.set('priority', priorityFilter)
    tagFilters.forEach(id => params.append('tag_id', id))
    try {
      const loaded = await api<Task[]>(`/tasks?${params}`)
      setTasks(loaded)
      if (selected) setSelected(loaded.find(task => task.id === selected.id) || null)
    } catch (error) { showNotice(readError(error)) }
    finally { setLoading(false) }
  }

  useEffect(() => { loadMeta().catch(error => showNotice(readError(error))) }, [])
  useEffect(() => { loadTasks() }, [view, search, priorityFilter, tagFilters])

  function showNotice(message: string) {
    setNotice(message); window.setTimeout(() => setNotice(''), 3500)
  }

  async function logout() {
    await del('/session').catch(() => undefined); onLogout()
  }

  async function complete(task: Task) {
    try {
      if (task.done) await post(`/tasks/${task.id}/reopen`)
      else await post(`/tasks/${task.id}/complete`)
      await Promise.all([loadTasks(), loadMeta()]); showNotice(task.done ? '任务已恢复' : '任务已完成')
    } catch (error) { showNotice(readError(error)) }
  }

  async function saveTask(id: string | null, input: TaskInput) {
    const saved = id ? await patch<Task>(`/tasks/${id}`, input) : await post<Task>('/tasks', input)
    setSelected(saved); setCreating(false); await Promise.all([loadTasks(), loadMeta()]); showNotice('任务已保存')
  }

  async function deleteTask(task: Task) {
    if (!confirm(`确定删除“${task.title}”吗？此操作无法撤销。`)) return
    await del(`/tasks/${task.id}`); setSelected(null); await Promise.all([loadTasks(), loadMeta()]); showNotice('任务已删除')
  }

  function startCreate(title = '', listID = selectedList?.id || '') {
    setCreationDraft({ title, listID }); setSelected(null); setCreating(true)
  }

  const editorTask = creating ? null : selected
  const isEditorOpen = creating || !!selected

  return <div class="app-shell">
    <Sidebar view={view} setView={value => { setView(value); setSelected(null); setCreating(false); setMenuOpen(false) }} lists={lists} counts={counts} user={user} onLogout={logout} onListsChanged={loadMeta} open={menuOpen} onClose={() => setMenuOpen(false)} />
    <main class={`content ${isEditorOpen ? 'with-editor' : ''}`}>
      <header class="topbar">
        <button class="icon-button mobile-only" onClick={() => setMenuOpen(true)} title="打开菜单"><Menu /></button>
        <div class="title-block"><p class="eyebrow">{view === 'today' ? new Intl.DateTimeFormat('zh-CN', { weekday: 'long', month: 'long', day: 'numeric' }).format(new Date()) : 'TASKS'}</p><h1>{title}</h1></div>
        <div class="top-actions">
          {searchOpen && <input class="search-input" value={search} onInput={e => setSearch(e.currentTarget.value)} placeholder="搜索任务" autofocus />}
          <button class={`icon-button ${searchOpen ? 'active' : ''}`} onClick={() => { setSearchOpen(!searchOpen); if (searchOpen) setSearch('') }} title="搜索"><Search /></button>
          <button class="icon-button" onClick={() => { setView('notifications'); setSelected(null) }} title="提醒通知"><Bell /></button>
        </div>
      </header>

      {view === 'settings' ? <SettingsPage notify={showNotice} /> : view === 'notifications' ? <NotificationsPage notify={showNotice} /> : <>
        <QuickAdd lists={lists} defaultList={selectedList?.id} onDetailedCreate={startCreate} onCreated={async () => { await Promise.all([loadTasks(), loadMeta()]); showNotice('任务已添加') }} />
        <TaskFilters tags={tags} priority={priorityFilter} selectedTags={tagFilters} onPriorityChange={setPriorityFilter} onTagsChange={setTagFilters} />
        {view === 'matrix'
          ? <MatrixView tasks={tasks} loading={loading} timeZone={user.timezone} activeTaskID={selected?.id} onSelect={task => { setSelected(task); setCreating(false) }} onComplete={complete} />
          : <section class="task-list" aria-label={title}>
            {loading ? <LoadingRows /> : tasks.length === 0 ? <EmptyState view={view} onCreate={() => startCreate()} /> : tasks.map(task =>
              <TaskRow key={task.id} task={task} active={selected?.id === task.id} onSelect={() => { setSelected(task); setCreating(false) }} onComplete={() => complete(task)} />
            )}
          </section>}
      </>}
    </main>

    {isEditorOpen && <TaskEditor task={editorTask} lists={lists} tags={tags} defaultList={creating ? creationDraft.listID : selectedList?.id} initialTitle={creating ? creationDraft.title : ''} onClose={() => { setSelected(null); setCreating(false) }} onSave={saveTask} onDelete={deleteTask} onTagsChanged={async () => setTags(await api<Tag[]>('/tags'))} notify={showNotice} />}

    {!isEditorOpen && view !== 'settings' && view !== 'notifications' && <button class="fab" title="新建任务" onClick={() => startCreate()}><Plus /></button>}
    <MobileNav view={view} setView={value => { setView(value); setSelected(null); setCreating(false) }} />
    {notice && <div class="toast" role="status">{notice}</div>}
  </div>
}

function Sidebar({ view, setView, lists, counts, user, onLogout, onListsChanged, open, onClose }: {
  view: View; setView: (view: View) => void; lists: List[]; counts: Record<string, number>; user: User; onLogout: () => void; onListsChanged: () => Promise<void>; open: boolean; onClose: () => void
}) {
  const [adding, setAdding] = useState(false); const [name, setName] = useState('')
  const nav: Array<[View, typeof Inbox, string]> = [['inbox', Inbox, '收件箱'], ['today', CalendarDays, '今天'], ['upcoming', Clock3, '近期'], ['all', ListIcon, '全部任务'], ['matrix', LayoutGrid, '四象限'], ['completed', CheckCircle2, '已完成']]
  async function addList(event: Event) { event.preventDefault(); if (!name.trim()) return; await post('/lists', { name }); setName(''); setAdding(false); await onListsChanged() }
  return <>
    {open && <button class="sidebar-scrim mobile-only" onClick={onClose} aria-label="关闭菜单" />}
    <aside class={`sidebar ${open ? 'open' : ''}`}>
      <div class="sidebar-head"><div class="brand-mark small"><Check size={20} strokeWidth={3} /></div><strong>拾光待办</strong><button class="icon-button mobile-only" onClick={onClose}><X /></button></div>
      <nav class="main-nav">
        {nav.map(([id, Icon, label]) => <button class={view === id ? 'active' : ''} onClick={() => setView(id)}><Icon /><span>{label}</span>{counts[id] > 0 && <em>{counts[id]}</em>}</button>)}
      </nav>
      <div class="section-label"><span>我的清单</span><button class="icon-button tiny" title="新建清单" onClick={() => setAdding(!adding)}><Plus /></button></div>
      {adding && <form class="inline-add" onSubmit={addList}><input value={name} onInput={e => setName(e.currentTarget.value)} placeholder="清单名称" autofocus /><button title="保存"><Check /></button></form>}
      <nav class="list-nav">
        {lists.map(list => <button class={view === `list:${list.id}` ? 'active' : ''} onClick={() => setView(`list:${list.id}`)}><i style={{ background: list.color }} /><span>{list.name}</span></button>)}
      </nav>
      <div class="sidebar-foot">
        <button onClick={() => setView('settings')} class={view === 'settings' ? 'active' : ''}><Settings /><span>设置</span></button>
        <div class="account"><span>{user.username.slice(0, 1).toUpperCase()}</span><div><strong>{user.username}</strong><small>个人空间</small></div><button class="icon-button tiny" onClick={onLogout} title="退出登录"><LogOut /></button></div>
      </div>
    </aside>
  </>
}

function QuickAdd({ lists, defaultList, onCreated, onDetailedCreate }: { lists: List[]; defaultList?: string; onCreated: () => void; onDetailedCreate: (title: string, listID: string) => void }) {
  const [title, setTitle] = useState(''); const [listID, setListID] = useState(defaultList || '')
  useEffect(() => setListID(defaultList || ''), [defaultList])
  async function submit(event: Event) { event.preventDefault(); if (!title.trim()) return; await post('/tasks', { title: title.trim(), list_id: listID, reminders: [], tag_ids: [] }); setTitle(''); onCreated() }
  function openDetailed() { onDetailedCreate(title.trim(), listID); setTitle('') }
  return <form class="quick-add" onSubmit={submit}><Plus /><input value={title} onInput={e => setTitle(e.currentTarget.value)} placeholder="快速添加任务" aria-label="任务标题" /><select value={listID} onChange={e => setListID(e.currentTarget.value)} aria-label="选择清单"><option value="">收件箱</option>{lists.map(list => <option value={list.id}>{list.name}</option>)}</select><button type="button" class="secondary detailed-add" onClick={openDetailed} title="新建详细任务"><FilePenLine /><span>详细</span></button><button class="primary compact" disabled={!title.trim()}>添加</button></form>
}

function TaskFilters({ tags, priority, selectedTags, onPriorityChange, onTagsChange }: {
  tags: Tag[]; priority: string; selectedTags: string[]; onPriorityChange: (value: string) => void; onTagsChange: (value: string[]) => void
}) {
  const active = priority !== '' || selectedTags.length > 0
  return <div class="task-filters" aria-label="任务筛选">
    <SlidersHorizontal />
    <select value={priority} onChange={event => onPriorityChange(event.currentTarget.value)} aria-label="按优先级筛选">
      <option value="">全部优先级</option>
      {priorityLabels.map((label, index) => <option value={index}>{label}</option>)}
    </select>
    {tags.length > 0 && <div class="filter-tags" aria-label="按标签筛选">
      {tags.map(tag => <button type="button" class={selectedTags.includes(tag.id) ? 'selected' : ''} aria-pressed={selectedTags.includes(tag.id)} onClick={() => onTagsChange(selectedTags.includes(tag.id) ? selectedTags.filter(id => id !== tag.id) : [...selectedTags, tag.id])}><i style={{ background: tag.color }} />{tag.name}</button>)}
    </div>}
    {active && <button type="button" class="icon-button tiny clear-filters" title="清除筛选" onClick={() => { onPriorityChange(''); onTagsChange([]) }}><X /></button>}
  </div>
}

function MatrixView({ tasks, loading, timeZone, activeTaskID, onSelect, onComplete }: {
  tasks: Task[]; loading: boolean; timeZone: string; activeTaskID?: string; onSelect: (task: Task) => void; onComplete: (task: Task) => void
}) {
  const quadrants = useMemo(() => {
    const today = dateKey(new Date(), timeZone)
    const groups: Task[][] = [[], [], [], []]
    tasks.forEach(task => {
      const important = task.priority >= 2
      const urgent = !!task.due_at && dateKey(new Date(task.due_at), timeZone) <= today
      groups[important ? (urgent ? 0 : 1) : (urgent ? 2 : 3)].push(task)
    })
    return groups
  }, [tasks, timeZone])
  const labels = ['重要且紧急', '重要不紧急', '紧急不重要', '不重要不紧急']
  if (loading) return <section class="matrix-loading"><LoadingRows /></section>
  return <section class="matrix-board" aria-label="任务四象限">
    {quadrants.map((items, index) => <section class={`matrix-quadrant q${index + 1}`}>
      <header><span>{labels[index]}</span><em>{items.length}</em></header>
      <div>{items.length === 0 ? <p class="quadrant-empty">暂无任务</p> : items.map(task => <TaskRow key={task.id} task={task} active={activeTaskID === task.id} onSelect={() => onSelect(task)} onComplete={() => onComplete(task)} />)}</div>
    </section>)}
  </section>
}

function TaskRow({ task, active, onSelect, onComplete }: { task: Task; active: boolean; onSelect: () => void; onComplete: () => void }) {
  const overdue = task.due_at && !task.done && new Date(task.due_at) < new Date()
  return <article class={`task-row ${active ? 'active' : ''} ${task.done ? 'done' : ''}`}>
    <button class="complete-button" onClick={event => { event.stopPropagation(); onComplete() }} title={task.done ? '恢复任务' : '完成任务'}>{task.done ? <CheckCircle2 /> : <Circle />}</button>
    <button class="task-main" onClick={onSelect}>
      <span class="task-title">{task.title}</span>
      <span class="task-meta">
        {task.due_at && <span class={overdue ? 'overdue' : ''}><CalendarDays />{formatDate(task.due_at)}</span>}
        {task.list && <span><i style={{ background: task.list.color }} />{task.list.name}</span>}
        {task.priority > 0 && <span class={`priority p${task.priority}`}>{priorityLabels[task.priority]}</span>}
        {task.tags.map(tag => <span class="task-tag"><i style={{ background: tag.color }} />{tag.name}</span>)}
        {task.recurrence_unit && <span><Repeat2 />重复</span>}
      </span>
    </button>
    <button class="icon-button tiny row-more" onClick={onSelect} title="任务详情"><MoreHorizontal /></button>
  </article>
}

function TaskEditor({ task, lists, tags, defaultList, initialTitle, onClose, onSave, onDelete, onTagsChanged, notify }: {
  task: Task | null; lists: List[]; tags: Tag[]; defaultList?: string; initialTitle: string; onClose: () => void; onSave: (id: string | null, input: TaskInput) => Promise<void>; onDelete: (task: Task) => Promise<void>; onTagsChanged: () => Promise<void>; notify: (message: string) => void
}) {
  const [title, setTitle] = useState(task?.title || initialTitle)
  const [notes, setNotes] = useState(task?.notes || '')
  const [listID, setListID] = useState(task?.list_id || defaultList || '')
  const [dueAt, setDueAt] = useState(toLocalInput(task?.due_at))
  const [priority, setPriority] = useState(task?.priority || 0)
  const [recurrenceUnit, setRecurrenceUnit] = useState(task?.recurrence_unit || '')
  const [recurrenceInterval, setRecurrenceInterval] = useState(task?.recurrence_interval || 1)
  const [selectedTags, setSelectedTags] = useState(task?.tags.map(tag => tag.id) || [])
  const [reminders, setReminders] = useState<Reminder[]>(task?.reminders.map(reminder => ({ ...reminder })) || [])
  const [saving, setSaving] = useState(false); const [newTag, setNewTag] = useState('')

  async function submit(event: Event) {
    event.preventDefault(); if (!title.trim()) return; setSaving(true)
    const normalizedReminders = reminders.map(reminder => reminder.kind === 'relative'
      ? { kind: 'relative' as const, offset_minutes: reminder.offset_minutes }
      : { kind: 'absolute' as const, trigger_at: reminder.trigger_at ? toISOString(toLocalInput(reminder.trigger_at)) : undefined })
    try { await onSave(task?.id || null, { title: title.trim(), notes, list_id: listID, due_at: toISOString(dueAt), priority, recurrence_unit: recurrenceUnit, recurrence_interval: recurrenceInterval, tag_ids: selectedTags, reminders: normalizedReminders }) }
    catch (error) { notify(readError(error)) } finally { setSaving(false) }
  }

  function addRelative(offset: number) { setReminders([...reminders, { kind: 'relative', offset_minutes: offset, trigger_at: dueAt ? new Date(new Date(dueAt).getTime() + offset * 60000).toISOString() : undefined }]) }
  async function createTag(event: Event) { event.preventDefault(); if (!newTag.trim()) return; const created = await post<Tag>('/tags', { name: newTag.trim() }); setSelectedTags([...selectedTags, created.id]); setNewTag(''); await onTagsChanged() }

  return <aside class="editor" aria-label={task ? '编辑任务' : '新建任务'}>
    <header class="editor-head"><button class="icon-button" onClick={onClose} title="关闭"><ChevronLeft class="mobile-only" /><X class="desktop-only" /></button><span>{task ? '任务详情' : '新建任务'}</span><button class="primary compact" form="task-form" disabled={saving || !title.trim()}><Save />{saving ? '保存中' : '保存'}</button></header>
    <form id="task-form" class="editor-form" onSubmit={submit}>
      <textarea class="title-input" value={title} onInput={e => setTitle(e.currentTarget.value)} placeholder="任务标题" rows={2} autofocus={!task} />
      <label class="field"><span><ListIcon />清单</span><select value={listID} onChange={e => setListID(e.currentTarget.value)}><option value="">收件箱</option>{lists.map(list => <option value={list.id}>{list.name}</option>)}</select></label>
      <label class="field datetime-field"><span><CalendarDays />截止时间</span><input type="datetime-local" value={dueAt} onInput={e => setDueAt(e.currentTarget.value)} /></label>
      <label class="field"><span><Archive />优先级</span><select value={priority} onChange={e => setPriority(Number(e.currentTarget.value))}>{priorityLabels.map((label, index) => <option value={index}>{label}</option>)}</select></label>
      <div class="field stacked"><span><Bell />提醒</span><div class="reminder-actions"><button type="button" onClick={() => addRelative(-10)} disabled={!dueAt}>提前10分钟</button><button type="button" onClick={() => addRelative(-60)} disabled={!dueAt}>提前1小时</button><button type="button" onClick={() => addRelative(-1440)} disabled={!dueAt}>提前1天</button><button type="button" onClick={() => setReminders([...reminders, { kind: 'absolute', trigger_at: new Date().toISOString() }])}>指定时间</button></div>
        {reminders.map((reminder, index) => <div class={`reminder-row${reminder.kind === 'absolute' ? ' absolute-reminder' : ''}`}>{reminder.kind === 'relative' ? <span>{relativeLabel(reminder.offset_minutes || 0)}</span> : <input type="datetime-local" value={toLocalInput(reminder.trigger_at)} onInput={e => { const next = [...reminders]; next[index] = { ...next[index], trigger_at: toISOString(e.currentTarget.value) }; setReminders(next) }} />}<button type="button" class="icon-button tiny" onClick={() => setReminders(reminders.filter((_, i) => i !== index))} title="删除提醒"><X /></button></div>)}
      </div>
      <div class="field stacked"><span><Repeat2 />重复</span><div class="repeat-fields"><input type="number" min="1" max="365" value={recurrenceInterval} disabled={!recurrenceUnit} onInput={e => setRecurrenceInterval(Number(e.currentTarget.value))} /><select value={recurrenceUnit} onChange={e => setRecurrenceUnit(e.currentTarget.value as typeof recurrenceUnit)}><option value="">不重复</option><option value="day">天</option><option value="week">周</option><option value="month">月</option><option value="year">年</option></select></div>{recurrenceUnit && !dueAt && <small class="field-error">重复任务需要设置截止时间</small>}</div>
      <div class="field stacked"><span><TagIcon />标签</span><div class="tag-picker">{tags.map(tag => <button type="button" class={selectedTags.includes(tag.id) ? 'selected' : ''} onClick={() => setSelectedTags(selectedTags.includes(tag.id) ? selectedTags.filter(id => id !== tag.id) : [...selectedTags, tag.id])}><i style={{ background: tag.color }} />{tag.name}</button>)}</div><div class="new-tag"><input value={newTag} onInput={e => setNewTag(e.currentTarget.value)} placeholder="新标签" /><button type="button" onClick={createTag} disabled={!newTag.trim()}><Plus />添加</button></div></div>
      <label class="notes-field"><span>备注</span><textarea value={notes} onInput={e => setNotes(e.currentTarget.value)} placeholder="补充说明、链接或相关信息…" rows={8} /></label>
    </form>
    {task && <footer class="editor-foot"><button class="danger-ghost" onClick={() => onDelete(task)}><Trash2 />删除任务</button><span>更新于 {formatDate(task.updated_at)}</span></footer>}
  </aside>
}

function relativeLabel(minutes: number) { const abs = Math.abs(minutes); if (abs % 1440 === 0) return `提前 ${abs / 1440} 天`; if (abs % 60 === 0) return `提前 ${abs / 60} 小时`; return `提前 ${abs} 分钟` }

function EmptyState({ view, onCreate }: { view: View; onCreate: () => void }) {
  return <div class="empty-state"><div><CheckCircle2 /></div><h2>{view === 'completed' ? '还没有完成的任务' : '这里已经清空了'}</h2><p>{view === 'today' ? '今天没有待办，给自己留一点空白。' : '创建一个任务，从下一件小事开始。'}</p>{view !== 'completed' && <button class="primary" onClick={onCreate}><Plus />新建任务</button>}</div>
}

function LoadingRows() { return <div class="loading-rows">{[1, 2, 3].map(() => <div><i /><span /></div>)}</div> }

function NotificationsPage({ notify }: { notify: (message: string) => void }) {
  const [items, setItems] = useState<Notification[]>([]); const [loading, setLoading] = useState(true)
  async function load() { setLoading(true); try { setItems(await api('/notifications')) } catch (e) { notify(readError(e)) } finally { setLoading(false) } }
  useEffect(() => { load() }, [])
  async function markRead(item: Notification) { await post(`/notifications/${item.id}/read`); await load() }
  return <section class="page-section"><div class="section-intro"><div><h2>提醒记录</h2><p>到期提醒会保留 90 天。</p></div><button class="secondary" onClick={load}><RefreshCw />刷新</button></div>{loading ? <LoadingRows /> : items.length === 0 ? <div class="simple-empty"><Bell /><p>暂无提醒</p></div> : <div class="notification-list">{items.map(item => <article class={item.read_at ? 'read' : ''}><span class="notification-icon"><Bell /></span><div><strong>{item.title}</strong><time>{formatDate(item.created_at)}</time></div>{!item.read_at && <button onClick={() => markRead(item)}>标为已读</button>}</article>)}</div>}</section>
}

function SettingsPage({ notify }: { notify: (message: string) => void }) {
  const [tokens, setTokens] = useState<TokenSummary[]>([]); const [deliveries, setDeliveries] = useState<Delivery[]>([])
  const [config, setConfig] = useState({ enabled: false, url: '', has_secret: false }); const [secret, setSecret] = useState(''); const [tokenName, setTokenName] = useState('AstrBot')
  const [newToken, setNewToken] = useState('')
  async function load() { const [t, c, d] = await Promise.all([api<TokenSummary[]>('/tokens'), api<any>('/integrations/astrbot'), api<Delivery[]>('/integrations/astrbot/deliveries')]); setTokens(t); setConfig(c); setDeliveries(d) }
  useEffect(() => { load().catch(e => notify(readError(e))) }, [])
  async function saveWebhook(event: Event) { event.preventDefault(); try { const payload: any = { enabled: config.enabled, url: config.url }; if (secret) payload.secret = secret; const saved = await put<any>('/integrations/astrbot', payload); setConfig(saved); setSecret(''); notify('AstrBot 配置已保存') } catch (e) { notify(readError(e)) } }
  async function createToken(event: Event) { event.preventDefault(); try { const result = await post<any>('/tokens', { name: tokenName, scopes: ['tasks:read', 'tasks:write'] }); setNewToken(result.token); await load() } catch (e) { notify(readError(e)) } }
  return <section class="settings-page">
    <div class="settings-block"><div class="settings-heading"><span><Webhook /></span><div><h2>AstrBot Webhook</h2><p>提醒到期时，以签名请求主动通知 AstrBot。</p></div></div><form class="settings-form" onSubmit={saveWebhook}><label class="switch-row"><span>启用提醒推送</span><input type="checkbox" checked={config.enabled} onChange={e => setConfig({ ...config, enabled: e.currentTarget.checked })} /></label><label>Webhook 地址<input type="url" value={config.url} onInput={e => setConfig({ ...config, url: e.currentTarget.value })} placeholder="http://127.0.0.1:6185/api/v1/plugins/extensions/…" /></label><label>共享密钥<input type="password" value={secret} onInput={e => setSecret(e.currentTarget.value)} placeholder={config.has_secret ? '已设置；留空保持不变' : '至少 24 个字符'} /></label><button class="primary"><Save />保存配置</button></form></div>
    <div class="settings-block"><div class="settings-heading"><span><KeyRound /></span><div><h2>API Token</h2><p>供未来 AstrBot 指令安全访问任务接口。</p></div></div><form class="token-create" onSubmit={createToken}><input value={tokenName} onInput={e => setTokenName(e.currentTarget.value)} placeholder="Token 名称" /><button class="secondary"><Plus />创建 Token</button></form>{newToken && <div class="token-reveal"><strong>请立即复制，关闭后不再显示</strong><code>{newToken}</code><button class="secondary" onClick={() => navigator.clipboard.writeText(newToken)}>复制</button></div>}<div class="token-list">{tokens.map(token => <article><div><strong>{token.name}</strong><small>{token.scopes.join(' · ')} · 创建于 {formatDate(token.created_at, false)}</small></div><button class="icon-button" title="撤销 Token" onClick={async () => { await del(`/tokens/${token.id}`); await load() }}><Trash2 /></button></article>)}</div></div>
    <div class="settings-block"><div class="settings-heading"><span><RefreshCw /></span><div><h2>最近投递</h2><p>Webhook 失败会自动重试 7 天。</p></div></div>{deliveries.length === 0 ? <p class="muted inset">暂无投递记录</p> : <div class="delivery-list">{deliveries.map(item => <article><i class={item.status} /><div><strong>{item.event_id}</strong><small>{item.status} · 尝试 {item.attempts} 次 · {formatDate(item.created_at)}</small>{item.last_error && <em>{item.last_error}</em>}</div>{item.status === 'dead' && <button onClick={async () => { await post(`/integrations/astrbot/deliveries/${item.id}/retry`); await load() }}>重试</button>}</article>)}</div>}</div>
  </section>
}

function MobileNav({ view, setView }: { view: View; setView: (view: View) => void }) {
  const items: Array<[View, typeof Inbox, string]> = [['inbox', Inbox, '收件箱'], ['today', CalendarDays, '今天'], ['upcoming', Clock3, '近期'], ['matrix', LayoutGrid, '象限'], ['all', ListIcon, '全部']]
  return <nav class="mobile-nav">{items.map(([id, Icon, label]) => <button class={view === id ? 'active' : ''} onClick={() => setView(id)}><Icon /><span>{label}</span></button>)}</nav>
}
