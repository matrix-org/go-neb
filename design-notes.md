Random TODOs as I think of them:

 - Whilst the `Expansions` bit on `plugins` is a lovely surprise, I find myself
   having to run the regexp twice because the callback does not give me any
   captured groups (just the matched string). I run it once so the callback
   fires, then run it again to actually extract the useful bits. This could
   be done better.

 - I'm not sure about the `Plugin(roomId string)` interface function. Python
   NEB could be configured to allow certain actions in certain rooms (e.g. only
   expand issues in this room, not that room). You can for sure do that using
   the given interface function, but you'd need to store those mappings
   yourself. Python NEB did it by giving you a key-value store which you could
   chuck config info for a room into: I wonder how useful that would be for
   Go-NEB?
