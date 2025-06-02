# Testing load_outputs=minimal

- target_a produces "output_1.txt"
- target_b pipes "output_1.txt" to "output_2.txt"
- target_c produces "output_3.txt" from "output_2.txt"

Test procedure:
- We first produce all outputs
- Delete output_1.txt
- Run all builds with load_outputs=minimal -> nothing should be loaded
- Alter the inputs to target_c and run again -> output_2.txt should be loaded
