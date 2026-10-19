export function createAPI(fetcher) {
 return async function api(path,options) {
  const response=await fetcher(path,options);
  const body=await response.json();
  if(!response.ok)throw new Error(body.error||"Falha na consulta.");
  return body;
 };
}
