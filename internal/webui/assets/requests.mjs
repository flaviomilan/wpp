export function createRequests() {
 let generation=0;
 const streams=new Map();
 return {
  invalidate(){generation++;streams.clear();},
  begin(stream){
   const sequence=(streams.get(stream)||0)+1;streams.set(stream,sequence);
   const epoch=generation;
   return {current:()=>generation===epoch&&streams.get(stream)===sequence};
  }
 };
}
