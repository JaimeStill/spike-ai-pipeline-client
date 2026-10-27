# integration testdata

The media the integration tier sends to set A. Both files are copied unchanged from the root
repository, spike-ai-pipeline (https://github.com/JaimeStill/spike-ai-pipeline),
`probe/testdata/` at commit `7263f14c0879b7b444b2d285a6a178a67381eb40`, whose README traces them
to spike-harness-driver and says how each was made.

- `shapes.png`: a red circle, a blue square, and a green triangle on white, 480 pixels square.
  `TestImageDescription` asks Gemma 4 26B-A4B to describe it.
- `phrase.wav`: one speaker saying "The access code is seven four two nine.", 16 kHz mono, a few
  seconds long. `TestAudioInChat` sends it to Gemma 4 E4B in chat.

With spike-ai-pipeline checked out beside this repository, this prints nothing when the copies
match their source:

```sh
for f in shapes.png phrase.wav; do
  git -C ../spike-ai-pipeline show 7263f14c0879b7b444b2d285a6a178a67381eb40:probe/testdata/$f | cmp - integration/testdata/$f
done
```
