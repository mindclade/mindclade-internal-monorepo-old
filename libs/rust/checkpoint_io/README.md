# `mindclade_checkpoint_io`

Transactional checkpoint sessions write verified shards into the artifact CAS, publish a
versioned manifest, and make the checkpoint visible only after a matching commit marker.
Readers verify commit integrity and every shard before restore.
