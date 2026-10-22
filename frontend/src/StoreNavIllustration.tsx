import { useId } from "react";

type NavIllustration = "catalog" | "search" | "heart" | "basket" | "person" | "reviews" | "messages";

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
      <linearGradient id={`${id}-shirt`} x1="0" y1="0" x2="1" y2="1"><stop stopColor="#efb18b" /><stop offset=".5" stopColor="#cb755b" /><stop offset="1" stopColor="#99513f" /></linearGradient>
      <linearGradient id={`${id}-steel`} x1="0" y1="0" x2="1" y2=".4"><stop stopColor="#f4f8f9" /><stop offset=".25" stopColor="#a3b5bf" /><stop offset=".5" stopColor="#e8eff2" /><stop offset=".75" stopColor="#8499a6" /><stop offset="1" stopColor="#c5d4db" /></linearGradient>
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
      <ellipse cx="24" cy="43" rx="19" ry="2.3" fill="#435663" opacity=".16" />
      <path d="M11 22 17 6q7-5 14 0l6 16" fill="none" stroke="#607885" strokeWidth="3.5" strokeLinejoin="round" />
      <path d="m11 21 7-15q6-4 12 0l7 15" fill="none" stroke="#dce9ee" strokeWidth="1.3" strokeLinejoin="round" />
      <path d="m18 6 1-2q5-2 10 0l1 2" fill="none" stroke="#da6b2e" strokeWidth="4.5" strokeLinecap="round" />
      <path d="M5 22 10 39l23 3 10-18-26-5Z" fill="#e0e9eb" fillOpacity=".45" stroke="#687f8c" strokeWidth="1.4" strokeLinejoin="round" />
      <path d="m17 20 3 19m6-18-1 19m10-18-5 19m9-16-6 16M9 25l2 13m3-14 2 15" fill="none" stroke={paint("steel")} strokeWidth="2.2" />
      <path d="m6 27 28 4 7-5M8 33l25 4 5-5m-28 6 23 3" fill="none" stroke="#8399a5" strokeWidth="1.5" strokeLinejoin="round" />
      <path d="m7 26 27 4 7-5M9 32l24 4 5-5" fill="none" stroke="#f5f9fa" strokeWidth=".8" strokeLinejoin="round" />
      <path d="m5 22 28 4 10-2-26-5Z" fill="none" stroke="#687f8c" strokeWidth="3" strokeLinejoin="round" />
      <path d="m5 21 28 4 10-2-26-5Z" fill="none" stroke="#e6f0f4" strokeWidth="1.2" strokeLinejoin="round" />
      <path d="m10 23 8-15q7-5 14 0l7 16" fill="none" stroke="#607885" strokeWidth="3" strokeLinejoin="round" />
      <path d="m10 22 9-14q6-4 12 0l8 15" fill="none" stroke="#dce9ee" strokeWidth="1" strokeLinejoin="round" />
      <path d="m19 8 1-2q5-2 10 0l1 2" fill="none" stroke="#e67836" strokeWidth="4" strokeLinecap="round" />
      <path d="M21 5.5q4-1 7 .2" fill="none" stroke="#ffc38a" strokeWidth="1.1" strokeLinecap="round" />
    </>}
    {(kind === "reviews" || kind === "messages") && <>
      <ellipse cx="24" cy="44" rx="18" ry="2.2" fill="#6c5433" opacity=".15" />
      <path d="M8 8q0-4 4-4h25q4 0 4 4v25q0 4-4 4H24l-12 8 2-8h-2q-4 0-4-4Z" fill="#ad7a45" stroke="#8d633b" strokeWidth=".9" strokeLinejoin="round" />
      <path d="M6 6q0-4 4-4h25q4 0 4 4v25q0 4-4 4H22l-12 8 2-8h-2q-4 0-4-4Z" fill={paint("glass")} stroke="#709b96" strokeWidth=".9" strokeLinejoin="round" />
      <path d="M10 12V7q0-1 2-1h19" fill="none" stroke="#fff" strokeWidth="1.9" strokeLinecap="round" opacity=".85" />
      {kind === "reviews" ? <>
      <path d="m23 8 3.2 6.5 7.2 1.1-5.2 5.1 1.2 7.2-6.4-3.4-6.4 3.4 1.2-7.2-5.2-5.1 7.2-1.1Z" fill={paint("gold")} stroke="#ac7337" strokeWidth=".9" strokeLinejoin="round" />
      <path d="m20 15 3-5 1.4 3.3" fill="none" stroke="#fff0c3" strokeWidth="1.2" strokeLinecap="round" strokeLinejoin="round" />
      </> : <path d="M14 13h17m-17 7h17m-17 7h10" fill="none" stroke="#477f77" strokeWidth="2.2" strokeLinecap="round" />}
      <path d="M17 31h13" fill="none" stroke="#719a8f" strokeWidth="1.7" strokeLinecap="round" />
    </>}
    {kind === "person" && <>
      <ellipse cx="24" cy="44" rx="18" ry="2" fill="#674431" opacity=".15" />
      <path d="M11 29 10 15C9-2 37-3 38 14l-1 16q-13 6-26-1Z" fill={paint("hair")} stroke="#493023" strokeWidth="1" />
      <path d="M6 43v-5c0-8 8-12 18-12s18 4 18 12v5q-18 5-36 0Z" fill={paint("shirt")} stroke="#a35d48" strokeWidth=".9" />
      <path d="M19 25v6q5 4 10 0v-6" fill={paint("skin")} stroke="#bd875e" strokeWidth=".7" />
      <path d="M17 30q7 10 14 0" fill="none" stroke="#f7d5b7" strokeWidth="2.7" strokeLinecap="round" />
      <path d="m13 37-1 7m23-7 1 7" stroke="#a35d48" strokeWidth="1" strokeLinecap="round" />
      <ellipse cx="13" cy="19" rx="2.2" ry="3.2" fill={paint("skin")} /><ellipse cx="35" cy="19" rx="2.2" ry="3.2" fill={paint("skin")} />
      <path d="M14 13v7c0 7 5 12 10 12s10-5 10-12v-7c-6 1-10-4-10-6-2 4-5 6-10 6Z" fill={paint("skin")} stroke="#bd875e" strokeWidth=".7" />
      <path d="M12 18c-2-9 3-15 12-15 10 0 14 7 12 15-5-2-10-6-12-11-2 6-7 9-12 11Z" fill={paint("hair")} />
      <path d="M14 12q2-7 9-7m5 1q5 2 6 7" fill="none" stroke="#ad7e50" strokeWidth="1.3" strokeLinecap="round" />
      <path d="m17 18 4-.6m6 0 4 .6" stroke="#694831" strokeWidth="1" strokeLinecap="round" />
      <ellipse cx="19" cy="21" rx="1.1" ry="1.4" fill="#403126" /><ellipse cx="29" cy="21" rx="1.1" ry="1.4" fill="#403126" />
      <circle cx="19.3" cy="20.6" r=".35" fill="#fff9e9" /><circle cx="29.3" cy="20.6" r=".35" fill="#fff9e9" />
      <path d="m24 21-1 3h2" fill="none" stroke="#be865d" strokeWidth=".8" strokeLinecap="round" />
      <ellipse cx="17.3" cy="25" rx="2" ry="1.2" fill="#e89b87" opacity=".7" /><ellipse cx="30.7" cy="25" rx="2" ry="1.2" fill="#e89b87" opacity=".7" />
      <path d="M21 27q3 3 6 0" fill="#fff2dd" stroke="#a56352" strokeWidth=".8" strokeLinecap="round" />
    </>}
  </svg>;
}
