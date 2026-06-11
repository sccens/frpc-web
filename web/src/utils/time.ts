// 把 RFC3339 时间戳渲染成适合界面的紧凑形式：
// 今天 → HH:mm；今年 → MM-DD HH:mm；更早 → YYYY-MM-DD HH:mm。
// 解析失败时原样返回，避免吞掉后端给出的任何非常规值。
export function formatTime(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  const pad = (n: number) => String(n).padStart(2, '0')
  const time = `${pad(date.getHours())}:${pad(date.getMinutes())}`
  const now = new Date()
  const sameDay =
    date.getFullYear() === now.getFullYear() &&
    date.getMonth() === now.getMonth() &&
    date.getDate() === now.getDate()
  if (sameDay) return time
  const monthDay = `${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
  if (date.getFullYear() === now.getFullYear()) return `${monthDay} ${time}`
  return `${date.getFullYear()}-${monthDay} ${time}`
}
