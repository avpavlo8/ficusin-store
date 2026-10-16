import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "./adminShared";
import type { MarketplaceReturn } from "./adminTypes";
export type ReturnAssessment = { condition:MarketplaceReturn["condition"]; comment:string; discountPercent:number; publicComment:string };
const assessmentJSON = (data:ReturnAssessment)=>JSON.stringify({...data,comment:data.comment.trim(),publicComment:data.publicComment.trim()});
const fromItem = (item:MarketplaceReturn):ReturnAssessment=>({condition:item.condition,comment:item.comment,discountPercent:item.discountPercent||0,publicComment:item.publicComment||""});

// Serialize snapshots and keep newer edits in the form while a request is in
// flight. Navigation can await flush, rather than discarding a dirty card.
export function useReturnAssessment(item:MarketplaceReturn,canEdit:boolean,onSaved:(item:MarketplaceReturn)=>void) {
 const [draft,setDraft]=useState(()=>fromItem(item));
 const [state,setState]=useState<"clean"|"dirty"|"saving"|"error">("clean");
 const [error,setError]=useState("");
 const value=useRef(draft), saved=useRef(assessmentJSON(draft)),inFlight=useRef<Promise<void>|null>(null);
 const savedCallback=useRef(onSaved);savedCallback.current=onSaved;
 // Adopt server-side state changes (for example preparation for sale) only
 // after local edits are saved. Receipt polling must never erase a dirty form.
 useEffect(()=>{
  const incoming=fromItem(item),json=assessmentJSON(incoming);
  if(!inFlight.current&&assessmentJSON(value.current)===saved.current&&json!==saved.current){saved.current=json;value.current=incoming;setDraft(incoming);setState("clean");}
 },[item,state]);
 const update=(patch:Partial<ReturnAssessment>)=>{const next={...value.current,...patch};value.current=next;setDraft(next);setState(assessmentJSON(next)===saved.current?"clean":"dirty");setError("");};
 const flush=useCallback(async()=>{
  if(!canEdit)return;
  while(inFlight.current)await inFlight.current;
  while(assessmentJSON(value.current)!==saved.current){
   const snapshot={...value.current},json=assessmentJSON(snapshot);
   if (!Number.isInteger(snapshot.discountPercent) || snapshot.discountPercent < 0 || snapshot.discountPercent > 100) {
    const cause = new Error("Укажите скидку целым числом от 0 до 100%."); setState("error"); setError(cause.message); throw cause;
   }
   setState("saving");setError("");
   const pending=api<{item:MarketplaceReturn}>(`/api/v1/admin/returns/${item.id}`,{method:"PATCH",body:json}).then(result=>{
    saved.current=assessmentJSON(fromItem(result.item));
    savedCallback.current(result.item);
    setState(assessmentJSON(value.current)===saved.current?"clean":"dirty");
   }).catch(async cause=>{
    // A lost response must not turn a successful write into a duplicate action.
    try {const result=await api<{item:MarketplaceReturn}>(`/api/v1/admin/returns/${item.id}`);if(assessmentJSON(fromItem(result.item))===json){saved.current=json;savedCallback.current(result.item);setState(assessmentJSON(value.current)===json?"clean":"dirty");return;}}catch{/* Keep the original error and the user's current fields. */}
    setState("error");setError(cause instanceof Error?cause.message:"Не удалось сохранить оценку");throw cause;
   });
   inFlight.current=pending;try{await pending;}finally{if(inFlight.current===pending)inFlight.current=null;}
  }
 },[canEdit,item.id]);
 useEffect(()=>{if(state!=="dirty")return;const timer=window.setTimeout(()=>void flush().catch(()=>{}),700);return()=>window.clearTimeout(timer);},[draft,state,flush]);
 useEffect(()=>{
  const dirty=()=>assessmentJSON(value.current)!==saved.current||Boolean(inFlight.current);
  const unload=(event:BeforeUnloadEvent)=>{if(dirty())event.preventDefault();};
  const hide=()=>{if(document.visibilityState==="hidden"&&dirty())void flush().catch(()=>{});};
  window.addEventListener("beforeunload",unload);document.addEventListener("visibilitychange",hide);
  return()=>{window.removeEventListener("beforeunload",unload);document.removeEventListener("visibilitychange",hide);if(dirty())void flush().catch(()=>{});};
 },[flush]);
 return {draft,update,flush,state,error};
}
