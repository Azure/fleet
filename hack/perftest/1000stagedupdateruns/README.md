# KubeFleet Performance/Scalability Test Utility: Creating 1K Staged Update Runs Concurrently

This directory contains a utility program that creates 1K staged update runs that run concurrently and polls
until their full completion. 

The program is added for the purpose of testing the performance and scalability of KubeFleet.

## Before you begin

* Set up 1K placements using the utility program in `../1000placements` before running this utility program.
* Make sure that all placements have been updated to use the `External` update strategy (via the staged update run APIs).
* Make sure that all member clusters are labelled with `env=[canary|staging|prod]` as appropriate so that KubeFleet
can assign them to their respective stages.

> If you have followed the instructions in `../../README.md` and have used the given scripts and utility programs
> to run the performance/scalability test, all the steps above should have been done for you already.

## Running the utility program

Run the commands below to run the utility program:

```bash
go run main.go
```

With the default setup, it might take 1-2 hours before all 1K staged update runs are fully completed. The programs
reports the progress in the output.

After the program completes, run the commands below to clean up the created 1K staged update runs:

```bash
CLEANUP=set go run main.go
```
