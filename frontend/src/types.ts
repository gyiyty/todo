export interface User { id: string; username: string; timezone: string }
export interface List { id: string; name: string; color: string; position: number }
export interface Tag { id: string; name: string; color: string }
export interface Reminder { id?: string; kind: 'absolute' | 'relative'; offset_minutes?: number; trigger_at?: string; sent_at?: string }
export interface Task {
  id: string; title: string; notes: string; list_id?: string; list?: List; due_at?: string;
  priority: number; done: boolean; completed_at?: string; archived: boolean;
  recurrence_unit: '' | 'day' | 'week' | 'month' | 'year'; recurrence_interval: number;
  tags: Tag[]; reminders: Reminder[]; created_at: string; updated_at: string;
}
export interface TaskInput {
  title?: string; notes?: string; list_id?: string; due_at?: string; priority?: number;
  archived?: boolean; recurrence_unit?: string; recurrence_interval?: number;
  tag_ids?: string[]; reminders?: Reminder[];
}
export interface Notification { id: string; task_id?: string; title: string; read_at?: string; created_at: string }
export interface TokenSummary { id: string; name: string; scopes: string[]; last_used_at?: string; created_at: string }
export interface Delivery { id: string; event_id: string; status: string; attempts: number; next_attempt_at: string; last_error: string; created_at: string }
