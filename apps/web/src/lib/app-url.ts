// An image roll does not carry new env vars: a node running this image under
// the previous manifest still names the public origin BETTER_AUTH_URL.
export function appUrl(): string {
  return (
    process.env.APP_URL ??
    process.env.BETTER_AUTH_URL ??
    'http://localhost:5273'
  )
}
