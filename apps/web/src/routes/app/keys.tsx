import { createFileRoute } from '@tanstack/react-router'
import { AppKeys } from '@lalternative/keys'

export const Route = createFileRoute('/app/keys')({
  component: KeysPage,
})

function KeysPage() {
  return (
    <AppKeys
      endpoint="/api/keys"
      copy={{
        title: "Clés d'application",
        description:
          'La clé que ton application présente sur ses appels à vvaves. Elle ne t’est montrée qu’une fois.',
      }}
    />
  )
}
