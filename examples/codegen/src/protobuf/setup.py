from setuptools import find_packages, setup

with open("requirements.txt") as f:
    required = f.read().splitlines()

setup(
    name="pb",
    version="0.1.0",
    packages=find_packages(),
    include_package_data=True,
    package_data={
        "": ["*.proto"],
    },
    install_requires=required,
)
