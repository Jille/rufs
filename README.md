rufs
====

RUFS is a filesystem that makes it easy to conveniently shares files with others. RUFS provides a FUSE mount that shows the files shared by everyone in your circles.

RUFS v2 introduced the concept of circles. Your RUFS client only interacts with people in overlapping circles.

In your fuse mount, you'll find three high level directories:

- /all: A combined view of all your circles together
- /$circle/all: A combined view of everyone in this circle
- /$circle/$username: The filesystem of one specific peer.

## Technical

The unique id of each circle is the hostname of the Discovery server. When starting your client, it will connect to the Discovery server of the circles you're in and the Discovery server will tell you how to connect to each of your peers.

Discovery servers are not aware of the files in a circle. Each time you list directory contents a Readdir RPC is sent to all your peers.

### Authentication

Each circle has a self-signed CA with the CommonName being the hostname of the Discovery server. The Discovery server is available over TLS with the CA as its certificate. Administrators can issue tokens (using `create_auth_token`), with which users can later `register` in a circle. Registration signs your public key with the circle's CA. You then use this certificate to talk to everyone else in the circle. This also guarantees you can't talk to people in circles you're not in.

### Configuration

By default, your configuration is stored in ~/.rufs2/. Inside, you'll find a `config.yaml` which lists your circles and which paths to share. You'll also find a folder `pki`, inside which there's one folder per circle you're a member of. For each circle we have the ca certificate of the circle and your private key + personal certificate.

By default your client connects to all circles listed in `config.yaml`. You can use `--config=~/.rufs2-foobar` for another config dir, or if you only want to subset your circles, you can specify a different config file in the same folder with `--config=~/.rufs2/anothercircle.yaml`.

### File transfers

There's two modes of transferring files: simple and fancy. In simple mode, you just send a ReadFile RPC to the peer you want the file from. You specify the offset and read size, and you'll get the data. The server can decide to trigger fancy mode at this point.

In fancy mode, the download is orchestrated through the DownloadOrchestrator. Everyone connects to the orchestrator and indicates which byte ranges they have and want, and the orchestrator tells people what data to send to another when.
