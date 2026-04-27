#!/bin/bash
acpx  --approve-all --model gemini-3-flash-preview gemini --no-wait "### PROMPT FOR 3 FLASH [TASK1] ###"
acpx --approve-all --model gemini-3-flash-preview gemini "### PROMPT FOR 3 FLASH [TASK2]"
