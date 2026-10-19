import { useId } from "react";

type NavIllustration = "catalog" | "search" | "heart" | "basket" | "person";

// Small, shaded illustrations stay identical on iOS and Android.
export function StoreNavIllustration({ kind }: { kind: NavIllustration }) {
  const id = useId().replaceAll(":", "");
  const paint = (name: string) => `url(#${id}-${name})`;
  return <svg viewBox="0 0 48 48" aria-hidden="true" focusable="false">
    <defs>
      <linearGradient id={`${id}-leaf`} x1="0" y1="0" x2="1" y2="1"><stop stopColor="#a2ce66" /><stop offset=".5" stopColor="#45863d" /><stop offset="1" stopColor="#235b35" /></linearGradient>
      <linearGradient id={`${id}-clay`} x1="0" y1="0" x2="1" y2=".3"><stop stopColor="#efad71" /><stop offset=".45" stopColor="#d77d40" /><stop offset="1" stopColor="#a84d28" /></linearGradient>
      <linearGradient id={`${id}-gold`} x1="0" y1="0" x2="1" y2="1"><stop stopColor="#ffe3a0" /><stop offset=".4" stopColor="#e9ac48" /><stop offset="1" stopColor="#ad6127" /></linearGradient>
      <linearGradient id={`${id}-glass`} x1="0" y1="0" x2="1" y2="1"><stop stopColor="#f5ffff" /><stop offset=".5" stopColor="#c8e7e7" /><stop offset="1" stopColor="#7bb9bd" /></linearGradient>
      <radialGradient id={`${id}-red`} cx=".32" cy=".2" r=".85"><stop stopColor="#ff9290" /><stop offset=".38" stopColor="#f44243" /><stop offset=".75" stopColor="#dc202e" /><stop offset="1" stopColor="#9d172b" /></radialGradient>
      <linearGradient id={`${id}-skin`} x1="0" y1="0" x2="1" y2=".5"><stop stopColor="#ffe4c7" /><stop offset=".6" stopColor="#edb78b" /><stop offset="1" stopColor="#ce8c64" /></linearGradient>
      <linearGradient id={`${id}-shirt`} x1="0" y1="0" x2="1" y2="1"><stop stopColor="#84ac87" /><stop offset=".5" stopColor="#447d61" /><stop offset="1" stopColor="#28503f" /></linearGradient>
      <linearGradient id={`${id}-hair`} x1="0" y1="0" x2="1" y2="1"><stop stopColor="#8c5d3b" /><stop offset=".5" stopColor="#533827" /><stop offset="1" stopColor="#30261e" /></linearGradient>
    </defs>
    {kind === "catalog" && <>
      <ellipse cx="24" cy="43" rx="19" ry="2.5" fill="#6c5433" opacity=".15" />
      <path d="M12 31V19m0 6C5 26 3 20 3 16c7 0 10 4 9 9Zm0-5c-1-7 3-11 8-13 2 7-1 12-8 13Z" fill={paint("leaf")} stroke="#37663a" strokeWidth="1" strokeLinejoin="round" />
      <path d="m5 29 2 11q5 3 10 0l2-11Z" fill={paint("gold")} stroke="#a97836" strokeWidth=".8" />
      <ellipse cx="12" cy="29" rx="7" ry="2" fill="#c18d48" />
      <path d="M31 32V10m0 13C20 23 17 14 19 9c9 0 14 7 12 14Zm0-6C27 7 33 3 38 2c4 8 1 13-7 15Zm0 12c0-10 5-14 14-14 0 9-6 15-14 14Z" fill={paint("leaf")} stroke="#37663a" strokeWidth="1.1" strokeLinejoin="round" />
      <path d="m22 13 9 10m5-17-5 11m10 1-10 11" fill="none" stroke="#d0df97" strokeWidth=".8" />
      <path d="m20 31 3 11q8 3 16 0l3-11Z" fill={paint("clay")} stroke="#a65c34" strokeWidth=".9" />
      <ellipse cx="31" cy="31" rx="11" ry="3" fill="#b86434" />
      <ellipse cx="31" cy="30.5" rx="8.5" ry="1.7" fill="#654731" />
      <path d="M31 31v-5" stroke="#3e743b" strokeWidth="2" />
      <path d="m25 35 1 5" stroke="#ffc99a" strokeWidth="1.7" strokeLinecap="round" opacity=".65" />
    </>}
    {kind === "search" && <>
      <ellipse cx="26" cy="43" rx="17" ry="2" fill="#6c5433" opacity=".15" />
      <path d="m30 29 12 11q2 2-1 4t-4 0L26 32" fill={paint("clay")} stroke="#995929" strokeWidth="1.2" />
      <path d="m31 31 7 8" stroke="#ffd4a0" strokeWidth="1.8" strokeLinecap="round" />
      <circle cx="20" cy="20" r="16" fill={paint("gold")} stroke="#a87738" strokeWidth="1" />
      <circle cx="20" cy="20" r="12.1" fill={paint("glass")} stroke="#a87d3a" strokeWidth="1.1" />
      <path d="M20 29V18m0 5c-6 0-9-4-8-8 6 0 9 3 8 8Zm0-4c0-6 4-9 9-9 0 7-3 10-9 9Z" fill={paint("leaf")} stroke="#46734a" strokeWidth=".8" />
      <path d="M10 17a10 10 0 0 1 7-7" fill="none" stroke="#fff" strokeWidth="2.7" strokeLinecap="round" opacity=".9" />
      <path d="M22 31a11 11 0 0 0 9-9" fill="none" stroke="#f5ffff" strokeWidth="1.2" strokeLinecap="round" opacity=".7" />
    </>}
    {kind === "heart" && <>
      <ellipse cx="24" cy="43" rx="16" ry="2.2" fill="#9d172b" opacity=".14" />
      <path d="M24 42C18 37 4 27 4 16 4 5 17 2 24 12 31 2 44 5 44 16c0 11-14 21-20 26Z" fill={paint("red")} stroke="#af2033" strokeWidth="1" strokeLinejoin="round" />
      <path d="M10 16c0-5 5-7 8-4" fill="none" stroke="#ffd2c8" strokeWidth="3.5" strokeLinecap="round" />
      <path d="M37 26c-2 4-7 9-11 12" fill="none" stroke="#a3172c" strokeWidth="1.7" strokeLinecap="round" opacity=".6" />
    </>}
    {kind === "basket" && <>
      <ellipse cx="24" cy="44" rx="19" ry="2" fill="#6c5433" opacity=".18" />
      <path d="M15 25c-4-12 0-21 9-21s13 9 9 21" fill="none" stroke="#925324" strokeWidth="5" />
      <path d="M15 22c-2-10 1-16 9-16s11 6 9 16" fill="none" stroke="#f0c580" strokeWidth="2" />
      <path d="M24 28V14m0 9c-8 0-12-5-11-10 7 0 12 4 11 10Zm0-5c0-7 4-11 11-11 0 8-4 12-11 11Z" fill={paint("leaf")} stroke="#37663a" strokeWidth="1" />
      <path d="m5 23 5 18q14 6 28 0l5-18Z" fill={paint("gold")} stroke="#a56b2d" strokeWidth="1.2" strokeLinejoin="round" />
      <path d="m11 27 3 14m4-14 1 16m6-16v17m6-17-1 16m7-16-3 14" stroke="#ae722f" strokeWidth="1.7" />
      <path d="M8 29q16 5 32 0M9 34q15 5 30 0M11 39q13 4 26 0" fill="none" stroke="#f9d58c" strokeWidth="2" />
      <path d="M7 30q17 5 34 0M9 35q15 5 30 0M10 40q14 5 28 0" fill="none" stroke="#9c632b" strokeWidth=".7" />
      <path d="M5 23q19 5 38 0" fill="none" stroke="#c28a42" strokeWidth="5" strokeLinecap="round" />
      <path d="M6 22q18 4 36 0" fill="none" stroke="#ffe1a3" strokeWidth="2" strokeLinecap="round" />
    </>}
    {kind === "person" && <>
      <ellipse cx="24" cy="44" rx="19" ry="2" fill="#254c3e" opacity=".15" />
      <path d="M5 43v-5c0-9 9-13 19-13s19 4 19 13v5q-19 5-38 0Z" fill={paint("shirt")} stroke="#315b48" strokeWidth="1" />
      <path d="M19 24v7q5 5 10 0v-7" fill={paint("skin")} stroke="#bd875e" strokeWidth=".7" />
      <path d="m17 29 7 7-6 4-4-10m17-1-7 7 6 4 4-10" fill="#b5d2b0" stroke="#4c7e5c" strokeWidth=".7" strokeLinejoin="round" />
      <path d="M24 36v10m-12-7v5m24-5v5" stroke="#234d3d" strokeWidth="1" />
      <circle cx="26" cy="41" r=".8" fill="#d7ddbd" />
      <ellipse cx="12.5" cy="18.5" rx="2.5" ry="4" fill={paint("skin")} />
      <ellipse cx="35.5" cy="18.5" rx="2.5" ry="4" fill={paint("skin")} />
      <path d="M12 16C9 4 17 1 25 2c10 0 14 7 11 18l-3 5H15Z" fill={paint("hair")} stroke="#493023" strokeWidth=".8" />
      <path d="M14 12v8c0 8 5 12 10 12s10-4 10-12v-8c-7 2-11-2-12-4-1 3-4 4-8 4Z" fill={paint("skin")} stroke="#bd875e" strokeWidth=".7" />
      <path d="M15 10c6 1 9-4 10-5m-8 0c-3 1-5 4-5 7" fill="none" stroke="#b08756" strokeWidth="1.3" strokeLinecap="round" />
      <path d="m17 17 4-.5m6 0 4 .5" stroke="#694831" strokeWidth="1.2" strokeLinecap="round" />
      <ellipse cx="19" cy="20" rx="1" ry="1.3" fill="#443329" /><ellipse cx="29" cy="20" rx="1" ry="1.3" fill="#443329" />
      <path d="m24 20-1 4h2" fill="none" stroke="#bb825a" strokeWidth=".8" strokeLinecap="round" />
      <ellipse cx="17.5" cy="24" rx="2" ry="1" fill="#e29a7d" opacity=".6" /><ellipse cx="30.5" cy="24" rx="2" ry="1" fill="#e29a7d" opacity=".6" />
      <path d="M21 27q3 2 6 0" fill="none" stroke="#9e604d" strokeWidth="1" strokeLinecap="round" />
    </>}
  </svg>;
}
