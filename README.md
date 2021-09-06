# Websockets console chat

My experiments with websockets.

For a client there is a few parameters:

- `server`  for servers address
- `name` for client username

For a server there is only one parameter `addr` to provide an address to listen on. 

## TODO
- create a middleware which will work as daemon sending/receiving messages for server/client
- handle errors better, provide end user with informative feedback when he cannot log in/etc...
- tests, tests, tests...
