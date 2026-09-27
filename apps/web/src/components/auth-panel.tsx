export function AuthPanel() {
  return (
    <div className="relative flex h-full w-full flex-col justify-end overflow-hidden bg-gradient-to-br from-emerald-50 via-sky-100 to-slate-200 md:p-9">
      <div
        aria-hidden="true"
        className="absolute -top-16 -right-16 size-64 rounded-full bg-emerald-300/40 blur-3xl"
      />
      <div
        aria-hidden="true"
        className="absolute -bottom-24 -left-12 size-72 rounded-full bg-sky-200/50 blur-3xl"
      />
      <div className="relative hidden space-y-3 md:block">
        <p className="text-[2rem] leading-[1.12] font-semibold tracking-[-0.02em] text-slate-900">
          La voix de tes applications.
        </p>
        <p className="max-w-[26ch] text-pretty text-sm leading-relaxed text-slate-900/75">
          Une clé, un texte, une lecture. Vvaves lit à voix haute ce que ton
          application écrit.
        </p>
      </div>
    </div>
  )
}
