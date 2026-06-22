# KubeFleet Performance/Scalability Test Utility: Creating 1K Placements Concurrently

This directory contains a utility program that creates 1K placements that run concurrently and polls
until their full completion. 

The program is added for the purpose of testing the performance and scalability of KubeFleet.

## Before you begin

* Set up a KubeFleet deployment.
* Make sure that all member clusters are labelled with `placement-group=N`, where N ranges from 0 to 9.
Placements created by this utility program will be each assigned an index X, ranging from 0 to 999,
and a placement of index X will select all member clusters that are labelled with `placement-group=X%10`.
* The program requires the following tools (aside from the Go runtime) to be installed:
    * `curl` (for retrieving pprof data)

> If you have followed the instructions in `../../README.md` and have used the given scripts and utility programs
> to run the performance/scalability test, all the steps above should have been done for you already.

## Running the utility program

Run the commands below to run the utility program:

```bash
RUN_NAME=example go run main.go
```

With the default setup, it might take ~1 hour before all 1K placements are fully completed. The program
reports the progress in the output.

After the program completes, run the commands below to clean up the created 1K placements:

```bash
CLEANUP=set go run main.go
```