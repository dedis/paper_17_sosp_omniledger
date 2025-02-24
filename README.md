# OmniLedger: A Secure, Scale-Out, Decentralized Ledger via Sharding

Thanks for your interest in this paper.
Due to various requests we put together this repository and hope you find it useful.
Unfortunately, as this is done in 2025, 7 years after the paper, there is not a clear
link between the figures in the paper and the simulations found here.
We hope you find it useful anyway.

- [PDF of the paper](https://ieeexplore.ieee.org/document/8418625)
- [video of the presentation](https://www.youtube.com/watch?v=f1pyaAZ7bj0)

## Running the simulations

You can use docker to run the simulations locally on your computer, or you can install golang-1.18:

```bash
docker run -ti -v $(PWD):/go/omni golang:1.18 bash
cd omni/omniledger
go build
./omniledger simple.toml
```

Here are the different simulation directories:

- `omniledger` - is the main simulation
- `byzcoin_ng` - byzcoin-NG simulation
- `byzcoin_ng2` - byzcoin-NG version 2 simulation
- `ntree` - simple tree
- `pbft` - comparison with PBFT
- `gossip/simulation` - gossip only

The other directories, `./bftcosi*`, `./byzcoin_lib`, `./cosi`, `./crypto`, are only libraries.

### Simulation of `ntree` and `pbft`

For the simulations in `ntree` and `pbft`, I couldn't find the simulation files and created
a more or less random file.
If you want to run these simulations, you'll have to change the `ntree/ntree.toml` and `pbft/pbft.toml`
files to fit your needs.

## Modifying the simulations

For each simulation, you will find a `name/name.toml` file with the parameters for the simulation.
You can modify these parameters to fit your purpose.

## Debugging errors

If the simulation doesn't work, you can try to add `-debug 3` and see if any of the debug outputs
shows what's wrong:

```bash
./omniledger -debug 3 simple.toml
```

It is important that the `-debug 3` comes before the simulation file.

## Running it on MacOSX

If you want to run it directly on MacOSX, without docker, you will have to adjust the 
limits of open files using the `./increase_filehandlers.sh` file.

## Large scale simulations

The large scale simulations with network delays were done on Deterlab, which does not exist in this form
anymore.
For this reason it is unfortunately not possible to perform these large scale simulations.

# Other remarks

There is quite a lot of duplication of the code in this repository and under `./vendor`.
However, that should not hinder the running of the simulations.

Making it run again has been done by Linus Gasser from c4dt.epfl.ch.