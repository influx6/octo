const octo = require("../octojs.js")


console.log("Testing HTTP Client Library");

const auth = octo.AuthCredentials("XScheme", "Rack", "4343121-GU", "Teddybear")
const attr = octo.Attr("http://localhost:5060", [], true)

const http = new octo.HTTPClient(auth, attr, function(data, tx, res){
  console.log("Data:", data.toString())
})

http.Do(octo.Message({
  name: "PUMP",
}))

http.Do(octo.Message({
  name: "REX",
}), console.log)
