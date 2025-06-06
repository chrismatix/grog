#!/bin/bash
[ ! -f output_1.txt ] && [ -f output_2.txt ] && [ ! -f output_3.txt ] || exit 1
