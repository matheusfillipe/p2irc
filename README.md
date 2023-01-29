# [p2ir.cf](https://p2ir.cf)
> POST TO IRC FOWARDER

Paste or send directly to irc server.

Check main.go constans for configuration.

## From command line

You can choose the server and the room or a DM (using `-` as a prefix). The server port defaults to 6667. Pay attention that to be able to send multiple lines you need to use `--data-binary` argument on linux curl.

```sh
command | curl -d @- p2ir.cf/irc.server/room
cat file | curl --data-binary @- p2ir.cf/irc.server/room
echo "hey greg" | curl -d @- p2ir.cf/irc.server.com/-greg
printf "hey greg\nhow are you\nbye" | curl --data-binary @- p2ir.cf/irc.server.com/room
printf "hey greg\nhow are you\nbye" | curl -H "NICK: samuel" --data-binary @- p2ir.cf/irc.server.com/room
```

## Github webhooks

Create a webhook and use `application/json` for your content-type header. Edit `main.go` `WEBHOOK_TEMPLATES` to have more notifications.
