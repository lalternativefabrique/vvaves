export function appUrl(): string {
  return process.env.APP_URL ?? 'http://localhost:5273'
}
